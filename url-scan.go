package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

type Crawler struct {
	Queue    chan string
	Visited  map[string]bool
	Mutex    sync.Mutex
	WG       sync.WaitGroup
	OutputCh chan string
	InScope  []string
	OutScope []string
}

func NewCrawler(inscope, outscope []string) *Crawler {
	return &Crawler{
		Queue:    make(chan string, 100),
		Visited:  make(map[string]bool),
		OutputCh: make(chan string),
		InScope:  inscope,
		OutScope: outscope,
	}
}

func (c *Crawler) Crawl(startURL string, outputFile string) {
	inScopeFile := outputFile + "_in_scope.txt"
	outScopeFile := outputFile + "_out_scope.txt"

	inScopeCh := make(chan string)
	outScopeCh := make(chan string)

	go c.writeToFiles(inScopeFile, outScopeFile, inScopeCh, outScopeCh)

	c.Queue <- startURL
	c.WG.Add(1)
	go c.worker(inScopeCh, outScopeCh)

	c.WG.Wait()
	close(inScopeCh)
	close(outScopeCh)
	log.Println("SCAN FINISHED")
}

func (c *Crawler) worker(inScopeCh, outScopeCh chan<- string) {
	for url := range c.Queue {
		c.processURL(url, inScopeCh, outScopeCh)
		c.WG.Done()
	}
}

func (c *Crawler) processURL(pageURL string, inScopeCh, outScopeCh chan<- string) {
    c.Mutex.Lock()
    if c.Visited[pageURL] {
        c.Mutex.Unlock()
        return
    }
    c.Visited[pageURL] = true
    c.Mutex.Unlock()

    fmt.Println("Crawling:", pageURL)
    resp, err := c.fetchURL(pageURL)
    if err != nil || resp.StatusCode != http.StatusOK {
        log.Printf("Error fetching URL %s: %v", pageURL, err)
        return
    }
    defer resp.Body.Close()

    doc, err := html.Parse(resp.Body)
    if err != nil {
        log.Printf("Error parsing HTML for URL %s: %v", pageURL, err)
        return
    }

    urls := c.extractLinks(pageURL, doc)
    for _, u := range urls {
        if c.isValidURL(u) {
            if c.isInScope(u) {
                log.Printf("In-scope URL found: %s", u)
                inScopeCh <- "In-scope: " + u
                c.Queue <- u
                c.WG.Add(1)
            } else {
                log.Printf("Out-of-scope URL found: %s", u)
                outScopeCh <- "Out-Of-Scope: " + u
            }
        } else {
            log.Printf("Invalid URL found: %s", u)
        }
        if isCodeFile(u) {
            c.extractURLsFromScript(u, inScopeCh, outScopeCh)
        }
    }
}

func (c *Crawler) extractLinks(base string, n *html.Node) []string {
    var urls []string
    if n.Type == html.ElementNode {
        switch n.Data {
        case "a", "link", "img", "iframe", "frame", "embed", "script", "source", "track", "video", "audio", "applet", "object", "area", "base", "input", "form":
            for _, a := range n.Attr {
                if a.Key == "href" || a.Key == "src" || a.Key == "data" || a.Key == "action" {
                    absoluteURL := c.formatURL(base, a.Val)
                    urls = append(urls, absoluteURL)
                }
            }
        case "meta":
            for _, a := range n.Attr {
                if a.Key == "content" && (strings.Contains(a.Val, "url=") || strings.Contains(a.Val, "URL=")) {
                    absoluteURL := c.formatURL(base, strings.Split(a.Val, "=")[1])
                    urls = append(urls, absoluteURL)
                }
            }
        case "button":
            for _, a := range n.Attr {
                if a.Key == "formaction" {
                    absoluteURL := c.formatURL(base, a.Val)
                    urls = append(urls, absoluteURL)
                }
            }
        case "blockquote", "del", "ins", "q":
            for _, a := range n.Attr {
                if a.Key == "cite" {
                    absoluteURL := c.formatURL(base, a.Val)
                    urls = append(urls, absoluteURL)
                }
            }
        case "command":
            for _, a := range n.Attr {
                if a.Key == "icon" {
                    absoluteURL := c.formatURL(base, a.Val)
                    urls = append(urls, absoluteURL)
                }
            }
        case "data":
            for _, a := range n.Attr {
                if a.Key == "value" {
                    absoluteURL := c.formatURL(base, a.Val)
                    urls = append(urls, absoluteURL)
                }
            }
        }
    }

    for child := n.FirstChild; child != nil; child = child.NextSibling {
        urls = append(urls, c.extractLinks(base, child)...)
    }
    return urls
}

func isCodeFile(u string) bool {
	codeExtensions := []string{
		".js", ".jsp", ".xml", ".html", ".htm", ".php", ".asp", ".aspx", ".css", ".json", 
		".txt", ".md", ".yaml", ".csv", ".doc", ".docx", ".pdf", ".ppt", ".pptx", ".xls", 
		".xlsx", ".ts", ".py", ".rb", ".java", ".c", ".h", ".cs", ".swift", ".kt", 
		".pl", ".sh", ".bat", ".go"}

	for _, ext := range codeExtensions {
		if strings.HasSuffix(u, ext) {
			return true
		}
	}
	return false
}

func (c *Crawler) extractURLsFromScript(scriptURL string, inScopeCh, outScopeCh chan<- string) {
    resp, err := c.fetchURL(scriptURL)
    if err != nil || resp.StatusCode != http.StatusOK {
        log.Printf("Error fetching script URL %s: %v", scriptURL, err)
        return
    }
    defer resp.Body.Close()

    bodyBytes, err := io.ReadAll(resp.Body)
    if err != nil {
        log.Printf("Error reading script body for URL %s: %v", scriptURL, err)
        return
    }
    body := string(bodyBytes)

    urlRegex := regexp.MustCompile(`https?://[^\s"']+`)
    urls := urlRegex.FindAllString(body, -1)

    for _, u := range urls {
        log.Printf("URL found in script: %s", u)
        if c.isInScope(u) {
            log.Printf("In-scope URL found: %s", u)
            inScopeCh <- "In-scope: " + u
        } else {
            log.Printf("Out-of-scope URL found: %s", u)
            outScopeCh <- "Out-Of-Scope: " + u
        }
    }
}

func (c *Crawler) fetchURL(pageURL string) (*http.Response, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return nil, err
	}

	// Custom user agent can be added here, chrome on windows for simplicity and acceptance
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.3")
	resp, err := client.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		return resp, nil
	}

	u, _ := url.Parse(pageURL)
	if u.Scheme == "http" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	req.URL = u
	resp, err = client.Do(req)
	return resp, err
}

func (c *Crawler) formatURL(base, href string) string {
	u, err := url.Parse(href)
	if err != nil || u.IsAbs() {
		return href
	}

	baseURL, err := url.Parse(base)
	if err != nil {
		return href
	}
	return baseURL.ResolveReference(u).String()
}

func (c *Crawler) isValidURL(u string) bool {
	match, _ := regexp.MatchString(`^https?://`, u)
	return match
}

func (c *Crawler) isInScope(u string) bool {
	parsedURL, err := url.Parse(u)
	if err != nil {
		return false
	}

	for _, scope := range c.InScope {
		if strings.HasSuffix(parsedURL.Host, scope) {
			return true
		}
	}

	for _, scope := range c.OutScope {
		if strings.HasSuffix(parsedURL.Host, scope) {
			return false
		}
	}

	return len(c.InScope) == 0
}

func (c *Crawler) writeToFiles(inScopeFile, outScopeFile string, inScopeCh, outScopeCh <-chan string) {
	inScope, err := os.Create(inScopeFile)
	if err != nil {
		log.Fatalf("Could not create file %s: %v", inScopeFile, err)
	}
	defer inScope.Close()

	outScope, err := os.Create(outScopeFile)
	if err != nil {
		log.Fatalf("Could not create file %s: %v", outScopeFile, err)
	}
	defer outScope.Close()

	inScope.WriteString("--IN SCOPE URLS:---\n")
	outScope.WriteString("--OUT OF SCOPE URLS:---\n")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for u := range inScopeCh {
			_, err := inScope.WriteString(u + "\n")
			if err != nil {
				log.Printf("Could not write URL %s to file: %v", u, err)
			}
		}
	}()

	go func() {
		defer wg.Done()
		for u := range outScopeCh {
			_, err := outScope.WriteString(u + "\n")
			if err != nil {
				log.Printf("Could not write URL %s to file: %v", u, err)
			}
		}
	}()

	wg.Wait()
}

func main() {
	urlPtr := flag.String("url", "", "URL to start crawling from")
	outputPtr := flag.String("output", "output.txt", "Output file to write URLs to")
	inScopePtr := flag.String("inscope", "", "Comma-separated list of in-scope base URLs")
	outScopePtr := flag.String("outscope", "", "Comma-separated list of out-of-scope base URLs")

	flag.Parse()

	if *urlPtr == "" {
		log.Fatal("Provide a starting URL using -url flag")
	}

	inScope := strings.Split(*inScopePtr, ",")
	outScope := strings.Split(*outScopePtr, ",")

	crawler := NewCrawler(inScope, outScope)
	crawler.Crawl(*urlPtr, *outputPtr)
}
