package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/cloudflare/cloudflare-go"
	"github.com/lixiangzhong/dnsutil"
	"github.com/miekg/dns"
)

type Domain struct {
	Name               string    `json:"name"`
	NameserversCorrect bool      `json:"nameservers_correct"`
	LastChecked        time.Time `json:"last_checked"`
}

var (
	domains           []Domain
	limboDomains      []string
	removedDomains    []string
	domainsLock       sync.RWMutex
	limboDomainLock   sync.RWMutex
	removedDomainLock sync.RWMutex
)

const (
	domainsFile        = "domains.json"
	limboDomainsFile   = "limbo_domains.txt"
	removedDomainsFile = "removed_domains.txt"
)

func main() {
	loadDomains()
	loadLimboDomains()
	loadRemovedDomains()

	go checkNameserversRoutine()
	go processLimboDomains()

	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/submit", handleSubmit)
	http.HandleFunc("/domains.json", handleDomainsJSON)

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func loadDomains() {
	data, err := os.ReadFile(domainsFile)
	if err != nil {
		log.Println("Error reading domains.json:", err)
		return
	}

	err = json.Unmarshal(data, &domains)
	if err != nil {
		log.Println("Error parsing domains.json:", err)
	}
}

func saveDomains() {
	domainsLock.RLock()
	defer domainsLock.RUnlock()

	data, err := json.MarshalIndent(domains, "", "  ")
	if err != nil {
		log.Println("Error marshaling domains:", err)
		return
	}

	err = os.WriteFile(domainsFile, data, 0644)
	if err != nil {
		log.Println("Error writing domains.json:", err)
	}
}

func loadLimboDomains() {
	file, err := os.Open(limboDomainsFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Println("Error opening limbo_domains.txt:", err)
		}
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		limboDomains = append(limboDomains, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Println("Error reading limbo_domains.txt:", err)
	}
}

func saveLimboDomains() {
	limboDomainLock.RLock()
	defer limboDomainLock.RUnlock()

	file, err := os.Create(limboDomainsFile)
	if err != nil {
		log.Println("Error creating limbo_domains.txt:", err)
		return
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, domain := range limboDomains {
		fmt.Fprintln(writer, domain)
	}
	writer.Flush()
}

func loadRemovedDomains() {
	file, err := os.Open(removedDomainsFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Println("Error opening removed_domains.txt:", err)
		}
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		removedDomains = append(removedDomains, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Println("Error reading removed_domains.txt:", err)
	}
}

func saveRemovedDomains() {
	removedDomainLock.RLock()
	defer removedDomainLock.RUnlock()

	file, err := os.Create(removedDomainsFile)
	if err != nil {
		log.Println("Error creating removed_domains.txt:", err)
		return
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, domain := range removedDomains {
		fmt.Fprintln(writer, domain)
	}
	writer.Flush()
}

func checkNameserversRoutine() {
	for {
		domainsLock.Lock()
		for i, domain := range domains {
			correct := checkNameservers(domain.Name)
			if !correct && domain.NameserversCorrect {
				// NS changed, move to removed domains
				removedDomainLock.Lock()
				removedDomains = append(removedDomains, domain.Name)
				removedDomainLock.Unlock()
				saveRemovedDomains()

				// Remove from active domains
				domains = append(domains[:i], domains[i+1:]...)
				i--
			} else {
				domains[i].NameserversCorrect = correct
				domains[i].LastChecked = time.Now()
			}
		}
		domainsLock.Unlock()
		saveDomains()
		time.Sleep(3 * time.Hour)
	}
}

func processLimboDomains() {
	for {
		limboDomainLock.Lock()
		for i, domain := range limboDomains {
			if checkNameservers(domain) {
				if addDomainToCloudflare(domain) {
					domainsLock.Lock()
					domains = append(domains, Domain{
						Name:               domain,
						NameserversCorrect: true,
						LastChecked:        time.Now(),
					})
					domainsLock.Unlock()
					saveDomains()

					// Remove from limbo
					limboDomains = append(limboDomains[:i], limboDomains[i+1:]...)
					i--
				}
			}
		}
		limboDomainLock.Unlock()
		saveLimboDomains()
		time.Sleep(1 * time.Hour)
	}
}

func checkNameservers(domain string) bool {
	expectedNameservers := []string{"ian.ns.cloudflare.com.", "vera.ns.cloudflare.com."}

	var dig dnsutil.Dig
	dig.SetDNS("1.1.1.1")

	msg, err := dig.GetMsg(dns.TypeNS, domain)
	if err != nil {
		log.Printf("Error looking up nameservers for %s: %v", domain, err)
		return false
	}

	var actualNameservers []string
	for _, answer := range msg.Answer {
		if ns, ok := answer.(*dns.NS); ok {
			actualNameservers = append(actualNameservers, ns.Ns)
		}
	}

	if len(actualNameservers) != len(expectedNameservers) {
		return false
	}

	for i, ns := range expectedNameservers {
		if actualNameservers[i] != ns {
			return false
		}
	}

	return true
}

func addDomainToCloudflare(domain string) bool {
	apiToken := os.Getenv("CLOUDFLARE_API_TOKEN")
	api, err := cloudflare.NewWithAPIToken(apiToken)
	if err != nil {
		log.Println("Error creating Cloudflare API client:", err)
		return false
	}

	// Create zone
	zone, err := api.CreateZone(context.Background(), domain, false, cloudflare.Account{}, "")
	if err != nil {
		log.Println("Error creating zone:", err)
		return false
	}

	log.Printf("Zone %s created\n", zone.Name)

	// Read and process master.zone file
	records, err := readMasterZoneFile("master.zone")
	if err != nil {
		log.Println("Error reading master.zone file:", err)
		return false
	}

	for _, record := range records {
		err := addDNSRecord(api, zone.ID, record)
		if err != nil {
			log.Printf("Error adding DNS record: %v\n", err)
			// Continue adding other records even if one fails
		}
	}

	log.Println("All DNS records created")
	return true
}

func readMasterZoneFile(filename string) ([][]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	// Remove header if present
	if len(records) > 0 && records[0][0] == "type" {
		records = records[1:]
	}

	return records, nil
}

func addDNSRecord(api *cloudflare.API, zoneID string, record []string) error {
	if len(record) != 5 {
		return fmt.Errorf("invalid record format: %v", record)
	}

	recordType := record[0]
	name := record[1]
	content := record[2]
	ttl, err := strconv.Atoi(record[3])
	if err != nil {
		return fmt.Errorf("invalid TTL: %s", record[3])
	}
	proxied, err := strconv.ParseBool(record[4])
	if err != nil {
		return fmt.Errorf("invalid proxied value: %s", record[4])
	}

	recordParams := cloudflare.CreateDNSRecordParams{
		Type:    recordType,
		Name:    name,
		Content: content,
		TTL:     ttl,
		Proxied: &proxied,
	}

	rc := cloudflare.ZoneIdentifier(zoneID)
	_, err = api.CreateDNSRecord(context.Background(), rc, recordParams)
	if err != nil {
		return fmt.Errorf("error creating DNS record: %v", err)
	}

	log.Printf("DNS record created: %s %s %s\n", recordType, name, content)
	return nil
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Domain Manager</title>
</head>
<body>
    <h1>Domain Manager</h1>
    <h2>Active Domains:</h2>
    <ul>
`
	domainsLock.RLock()
	for _, domain := range domains {
		html += fmt.Sprintf("<li>%s - Nameservers Correct: %v - Last Checked: %s</li>",
			domain.Name, domain.NameserversCorrect, domain.LastChecked.Format(time.RFC3339))
	}
	domainsLock.RUnlock()

	html += `
    </ul>
    <h2>Limbo Domains:</h2>
    <ul>
`
	limboDomainLock.RLock()
	for _, domain := range limboDomains {
		html += fmt.Sprintf("<li>%s</li>", domain)
	}
	limboDomainLock.RUnlock()

	html += `
    </ul>
    <p><a href="/domains.json">View JSON</a></p>
    <p><a href="/submit">Submit Domain</a></p>
</body>
</html>
`
	fmt.Fprint(w, html)
}

func handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "Error parsing form", http.StatusBadRequest)
			return
		}

		domain := r.FormValue("domain")
		if domain == "" {
			http.Error(w, "Domain is required", http.StatusBadRequest)
			return
		}

		limboDomainLock.Lock()
		limboDomains = append(limboDomains, domain)
		limboDomainLock.Unlock()
		saveLimboDomains()

		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Submit Domain</title>
</head>
<body>
    <h1>Submit Domain</h1>
    <p>Please ensure your domain's nameservers are set to:</p>
    <ol>
        <li>ian.ns.cloudflare.com</li>
        <li>vera.ns.cloudflare.com</li>
    </ol>
    <form method="POST">
        <input type="text" name="domain" placeholder="Enter your domain" required>
        <input type="submit" value="Submit">
    </form>
    <p><a href="/">Back to Home</a></p>
</body>
</html>
`
	fmt.Fprint(w, html)
}

func handleDomainsJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	domainsLock.RLock()
	json.NewEncoder(w).Encode(domains)
	domainsLock.RUnlock()
}
