# golang-url-crawler
A script to fetch domains and subdomains in a target URL logging both in scope and out of scope URLs.

Steps:

1. Upload golang file
2. Make sure golang is installed by using: 
3. go build url-crawler.go
4. chmod 777 *
5. ./url-scan -url="https://hackerone.com/" -output="output-hackerone.txt" -inscope="hackerone.com"
