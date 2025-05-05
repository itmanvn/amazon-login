package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

type Account struct {
	Email    string
	Password string
}

type Proxy struct {
	Server string
	Port   string
}

type AccountProxyMapping struct {
	Email string `json:"email"`
	Proxy Proxy  `json:"proxy"`
}

// Load accounts from file
func loadAccounts(filePath string) ([]Account, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var accounts []Account
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) == 2 {
			accounts = append(accounts, Account{Email: parts[0], Password: parts[1]})
		}
	}
	return accounts, scanner.Err()
}

// Load proxies from file
func loadProxies(filePath string) ([]Proxy, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var proxies []Proxy
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) == 2 {
			proxies = append(proxies, Proxy{Server: parts[0], Port: parts[1]})
		}
	}
	return proxies, scanner.Err()
}

// Load account-to-proxy mapping
func loadMapping(filePath string) (map[string]Proxy, error) {
	data, err := ioutil.ReadFile(filePath)
	if os.IsNotExist(err) {
		return make(map[string]Proxy), nil
	}
	if err != nil {
		return nil, err
	}
	var mappings []AccountProxyMapping
	if err := json.Unmarshal(data, &mappings); err != nil {
		return nil, err
	}
	mapping := make(map[string]Proxy)
	for _, m := range mappings {
		mapping[m.Email] = m.Proxy
	}
	return mapping, nil
}

// Save account-to-proxy mapping
func saveMapping(filePath string, mapping map[string]Proxy) error {
	var mappings []AccountProxyMapping
	for email, proxy := range mapping {
		mappings = append(mappings, AccountProxyMapping{Email: email, Proxy: proxy})
	}
	data, err := json.MarshalIndent(mappings, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filePath, data, 0644)
}

// Load cookies
func loadCookies(email string) ([]*chromedp.Cookie, error) {
	cookieFile := fmt.Sprintf("cookies_%s.json", strings.Replace(email, "@", "_", -1))
	data, err := ioutil.ReadFile(cookieFile)
	if err != nil {
		return nil, err
	}
	var cookies []*chromedp.Cookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		return nil, err
	}
	return cookies, nil
}

// Select random account and get its proxy
func selectRandomAccountAndProxy(accounts []Account, proxies []Proxy, mapping map[string]Proxy) (Account, Proxy) {
	rand.Seed(time.Now().UnixNano())
	account := accounts[rand.Intn(len(accounts))]
	proxy, exists := mapping[account.Email]
	if !exists {
		proxy = proxies[rand.Intn(len(proxies))]
		mapping[account.Email] = proxy
	}
	return account, proxy
}

// Perform login
func performLogin(ctx context.Context, account Account, cookiesChan chan<- []*chromedp.Cookie) error {
	return chromedp.Run(ctx,
		chromedp.Navigate("https://www.amazon.com/ap/signin"),
		chromedp.Sleep(2*time.Second),
		chromedp.WaitVisible(`#ap_email`, chromedp.ByID),
		chromedp.SendKeys(`#ap_email`, account.Email, chromedp.ByID),
		chromedp.Click(`#continue`, chromedp.ByID),
		chromedp.Sleep(1*time.Second),
		chromedp.WaitVisible(`#ap_password`, chromedp.ByID),
		chromedp.SendKeys(`#ap_password`, account.Password, chromedp.ByID),
		chromedp.Click(`#signInSubmit`, chromedp.ByID),
		chromedp.Sleep(2*time.Second),
		chromedp.WaitVisible(`#nav-link-accountList`, chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			cookies, err := chromedp.Cookies().Do(ctx)
			if err != nil {
				return err
			}
			cookiesChan <- cookies
			return nil
		}),
	)
}

func main() {
	// Load accounts, proxies, and mapping
	accounts, err := loadAccounts("accounts.txt")
	if err != nil {
		log.Fatalf("Error loading accounts: %v", err)
	}
	proxies, err := loadProxies("proxies.txt")
	if err != nil {
		log.Fatalf("Error loading proxies: %v", err)
	}
	mapping, err := loadMapping("account_proxy_mapping.json")
	if err != nil {
		log.Fatalf("Error loading mapping: %v", err)
	}

	// Select random account and its proxy
	account, proxy := selectRandomAccountAndProxy(accounts, proxies, mapping)
	log.Printf("Using account: %s, proxy: %s:%s", account.Email, proxy.Server, proxy.Port)

	// Create chromedp context
	ctx, cancel := chromedp.NewContext(context.Background(), chromedp.WithLogf(log.Printf))
	defer cancel()

	// Configure headless Chrome with proxy
	ctx, cancel = chromedp.NewExecAllocator(ctx,
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Headless,
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"),
		chromedp.WindowSize(1920, 1080),
		chromedp.ProxyServer(fmt.Sprintf("http://%s:%s", proxy.Server, proxy.Port)),
	)
	defer cancel()

	// Channel for cookies
	cookiesChan := make(chan []*chromedp.Cookie, 1)

	// Check for proxy failure
	proxyFailed := false
	err = chromedp.Run(ctx,
		chromedp.Navigate("https://www.amazon.com"),
		chromedp.Sleep(2*time.Second),
	)
	if err != nil {
		log.Printf("Proxy %s:%s failed: %v", proxy.Server, proxy.Port, err)
		proxyFailed = true
	}

	// Replace proxy if failed
	if proxyFailed {
		log.Println("Selecting new proxy")
		newProxy := proxies[rand.Intn(len(proxies))]
		for newProxy.Server == proxy.Server && newProxy.Port == proxy.Port {
			newProxy = proxies[rand.Intn(len(proxies))]
		}
		proxy = newProxy
		mapping[account.Email] = proxy
		log.Printf("New proxy for %s: %s:%s", account.Email, proxy.Server, proxy.Port)
		// Reconfigure context with new proxy
		ctx, cancel = chromedp.NewContext(context.Background(), chromedp.WithLogf(log.Printf))
		defer cancel()
		ctx, cancel = chromedp.NewExecAllocator(ctx,
			chromedp.NoFirstRun,
			chromedp.NoDefaultBrowserCheck,
			chromedp.Headless,
			chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"),
			chromedp.WindowSize(1920, 1080),
			chromedp.ProxyServer(fmt.Sprintf("http://%s:%s", proxy.Server, proxy.Port)),
		)
		defer cancel()
	}

	// Check for existing cookies
	cookieFile := fmt.Sprintf("cookies_%s.json", strings.Replace(account.Email, "@", "_", -1))
	if cookies, err := loadCookies(account.Email); err == nil {
		log.Printf("Cookies found for %s, attempting to reuse", account.Email)
		err = chromedp.Run(ctx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				for _, cookie := range cookies {
					err := chromedp.SetCookie(cookie.Name, cookie.Value, cookie.Path, cookie.Domain, cookie.Expires, cookie.Secure, cookie.HttpOnly).Do(ctx)
					if err != nil {
						return err
					}
				}
				return nil
			}),
			chromedp.Navigate("https://www.amazon.com"),
			chromedp.Sleep(2*time.Second),
			chromedp.ActionFunc(func(ctx context.Context) error {
				var exists bool
				err := chromedp.Evaluate(`document.querySelector("#nav-link-accountList") !== null`, &exists).Do(ctx)
				if err == nil && exists {
					log.Printf("Cookies valid for %s", account.Email)
					cookiesChan <- cookies
					return nil
				}
				log.Printf("Cookies invalid for %s, performing fresh login", account.Email)
				return performLogin(ctx, account, cookiesChan)
			}),
		)
		if err != nil {
			log.Printf("Error using cookies: %v", err)
			return
		}
	} else {
		log.Printf("No cookies found for %s, performing fresh login", account.Email)
		err = performLogin(ctx, account, cookiesChan)
		if err != nil {
			log.Printf("Login failed for %s: %v", account.Email, err)
			var html string
			chromedp.OuterHTML("html", &html).Do(ctx)
			log.Println("Page source:", html)
			return
		}
	}

	// Save cookies
	cookies := <-cookiesChan
	cookieData, err := json.Marshal(cookies)
	if err != nil {
		log.Printf("Error marshaling cookies: %v", err)
		return
	}
	if err := ioutil.WriteFile(cookieFile, cookieData, 0644); err != nil {
		log.Printf("Error saving cookies: %v", err)
		return
	}

	// Save updated mapping
	if err := saveMapping("account_proxy_mapping.json", mapping); err != nil {
		log.Printf("Error saving mapping: %v", err)
		return
	}

	log.Printf("Success for %s! Cookies saved to %s, mapping updated", account.Email, cookieFile)
}