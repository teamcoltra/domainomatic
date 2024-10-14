# DomainOmatic

DomainOmatic is a Go-based web application that automates the process of managing domains with Cloudflare. It provides a simple interface for users to submit domains, checks nameservers, and automatically adds domains to Cloudflare with preconfigured DNS records.

## Features

- Web interface for domain submission
- Automatic nameserver verification
- Cloudflare integration for domain and DNS record management
- Limbo system for domains pending nameserver configuration
- Periodic checking of domain nameservers
- Removal of domains with changed nameservers

## Prerequisites

- Go 1.16 or higher
- Cloudflare account and API token

## Installation

1. Clone the repository:
   ```
   git clone https://github.com/teamcoltra/domainomatic.git
   cd domainomatic
   ```

2. Install dependencies:
   ```
   go get github.com/cloudflare/cloudflare-go
   go get github.com/lixiangzhong/dnsutil
   go get github.com/miekg/dns
   ```

3. Set up your Cloudflare API token as an environment variable:
   ```
   export CLOUDFLARE_API_TOKEN=your_api_token_here
   ```

4. Create a `master.zone` file in the project root directory (see example below).

5. Build the application:
   ```
   go build
   ```

## Usage

1. Start the server:
   ```
   ./domainomatic
   ```

2. Access the web interface at `http://localhost:8080`

3. Use the web interface to submit domains. Submitted domains will be placed in limbo until their nameservers are verified.

4. The application will automatically process limbo domains every hour, adding them to Cloudflare if the nameservers are correct.

5. Active domains are checked every 3 hours to ensure their nameservers haven't changed.

## Configuration

### master.zone File

The `master.zone` file is a CSV file that defines the DNS records to be created for each domain added to Cloudflare. Here's an example of what it should look like:

```csv
type,name,content,ttl,proxied
A,@,192.0.2.1,1,true
CNAME,www,example.com,1,true
MX,@,mail.example.com,3600,false
TXT,@,v=spf1 include:_spf.example.com ~all,1,false
```

- `type`: DNS record type (A, CNAME, MX, TXT, etc.)
- `name`: The name of the record (use @ for root domain)
- `content`: The content of the record (IP address, target domain, etc.)
- `ttl`: Time to Live in seconds (use 1 for automatic)
- `proxied`: Whether the record should be proxied through Cloudflare (true/false)

## File Structure

- `domains.json`: Stores active domains
- `limbo_domains.txt`: Stores domains pending nameserver verification
- `removed_domains.txt`: Stores domains that have been removed due to nameserver changes

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details.