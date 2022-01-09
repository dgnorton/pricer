package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/anaskhan96/soup"
	"github.com/chromedp/chromedp"
)

type VendorItem struct {
	VendorName string `json:"vendor-name"`
	URL        string `json:"url"`
	Price      int    `json:"price"`    // USD cents
	Shipping   int    `json:"shipping"` // USD cents
}

func NewVendorItem(vendorName, url string) *VendorItem {
	return &VendorItem{
		VendorName: vendorName,
		URL:        url,
	}
}

func (vi *VendorItem) TotalCost() int {
	totalCost := vi.Price
	if vi.Shipping > -1 {
		totalCost += vi.Shipping
	}
	return totalCost
}

type Item struct {
	Description string        `json:"description"`
	Quantity    int           `json:"quantity"`
	VendorItems []*VendorItem `json:"vendor-items"`
}

func NewItem(desc string, qty int) *Item {
	return &Item{
		Description: desc,
		Quantity:    qty,
		VendorItems: []*VendorItem{},
	}
}

func LoadItems(r io.Reader) ([]*Item, error) {
	colNames := []string{}
	items := []*Item{}

	br := bufio.NewReader(r)

	for lineNum := 1; ; lineNum++ {
		// Read a line from input.
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return items, nil
			}
			return items, err
		}

		// Split the line at the tabs.
		vals := strings.Split(line, "\t")
		if len(vals) < 3 {
			return items, fmt.Errorf("error [line %d]: need at least 3 columns, only found %d", lineNum, len(vals))
		}

		if lineNum == 1 {
			// First line contains column names.
			colNames = append(colNames, vals...)
			continue
		}

		// Get the description and quantity.
		desc := strings.TrimSpace(vals[0])

		qty, err := strconv.ParseInt(vals[1], 10, 64)
		if err != nil {
			return items, fmt.Errorf("error [line %d]: cannot parse quantity: %s", lineNum, err)
		}

		// Create a new item.
		item := NewItem(desc, int(qty))

		// Set the vendor info.
		for i, url := range vals[2:] {
			vendItem := NewVendorItem(strings.TrimSpace(colNames[i+2]), strings.TrimSpace(url))
			item.VendorItems = append(item.VendorItems, vendItem)
		}

		items = append(items, item)
	}
}

func LookupVendorPrices(items []*Item) error {
	jobs := make(chan *BrowserJob)
	results := make(chan *BrowserJobResult)

	numWorkers := 10
	wg := &sync.WaitGroup{}
	wg.Add(numWorkers)

	// Start browser workers in the background.
	for n := 0; n < numWorkers; n++ {
		go chromeBrowser(jobs, results, wg)
	}

	// Start another background worker to feed jobs to the background browsers
	// and close the jobs and results channels when all the background browsers
	// are done processing jobs.
	jobsInProgress := map[*BrowserJob]*VendorItem{}
	var mu sync.Mutex

	go func() {
		for _, item := range items {
			for _, mi := range item.VendorItems {
				job := &BrowserJob{URL: mi.URL}
				mu.Lock()
				jobsInProgress[job] = mi
				mu.Unlock()
				jobs <- job
			}
		}

		// Tell background workers to exit as soon as the jobs queue is empty.
		close(jobs)
		// Wait for all background workers to exit.
		wg.Wait()
		// Tell the results processor that there will be no more results.
		close(results)
	}()

	// Process results from the background browser workers.
	for result := range results {
		mu.Lock()
		mi := jobsInProgress[result.Job]
		delete(jobsInProgress, result.Job)
		mu.Unlock()

		if mi == nil {
			// TODO: track down how this is possible
			fmt.Fprintf(os.Stderr, "error: no vendor item matches result: job.URL = %q\n", result.Job.URL)
			continue
		}

		if result.Err != nil {
			// TODO: something better than dumping out to the screen?
			fmt.Fprintf(os.Stderr, "error: %s: %s\n", mi.URL, result.Err)
		}

		price, shipping, err := GetPriceAndShipping(result.Job.URL, result.HTML)
		if err != nil {
			// TODO: something better than dumping out to the screen?
			fmt.Fprintf(os.Stderr, "error: %s: %s\n", mi.URL, err)
		}

		mi.Price = price
		mi.Shipping = shipping
	}

	return nil
}

func GetPriceAndShipping(url, html string) (price, shipping int, err error) {
	// Parse the response HTML using the function that understands the
	// vendor's HTML.
	url = strings.ToLower(url)

	if strings.Contains(html, "https://www.banggood.com") {
		return GetBanggoodPriceAndShipping(html)
	} else if strings.Contains(html, "https://www.aliexpress.com") {
		return GetAliExpressPriceAndShipping(html)
	}

	return -1, -1, fmt.Errorf("unsupported vendor: %s", url)
}

func GetBanggoodPriceAndShipping(html string) (price, shipping int, err error) {
	doc := soup.HTMLParse(html)

	// Parse the item price.
	elem := doc.Find("span", "class", "main-price")
	if elem.Pointer == nil {
		return -1, -1, fmt.Errorf("couldn't find <span class=\"main-price\"")
	}

	priceStr := elem.Text()
	if priceStr == "" {
		return -1, -1, errors.New("price element was an empty string")
	} else if priceStr == "US$00.00" {
		return -1, -1, errors.New("found price of US$00.00 - may not have waited long enough for the price to update in headless Chrome")
	}

	priceStr = strings.TrimPrefix(priceStr, "US$")

	pricef64, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return -1, -1, fmt.Errorf("parsing %s: %s", priceStr, err)
	}
	price = int(pricef64 * 100)

	// Parse the shipping price.
	shippingStr := "0.00"

	elem = doc.Find("em", "class", "shipping-price-em")
	if elem.Pointer != nil {
		txt := elem.Text()
		if txt != "" {
			shippingStr = strings.TrimPrefix(txt, "US$")
		}
	}

	shippingf64, err := strconv.ParseFloat(shippingStr, 64)
	if err != nil {
		return -1, -1, err
	}
	shipping = int(shippingf64 * 100.0)

	return price, shipping, nil
}

func GetAliExpressPriceAndShipping(html string) (price, shipping int, err error) {
	return -1, -1, nil
}

type BrowserJob struct {
	URL string
}

type BrowserJobResult struct {
	Job  *BrowserJob
	HTML string
	Err  error
}

func chromeBrowser(jobs chan *BrowserJob, results chan *BrowserJobResult, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()

		ctx, cancel := chromedp.NewContext(context.Background())
		defer cancel()

		for job := range jobs {
			if job == nil {
				continue
			}

			var html string

			if err := chromedp.Run(ctx,
				chromedp.Navigate(job.URL),
				// TODO: fix this ugly hack - hopefully there's a better way
				chromedp.Sleep(500*time.Millisecond),
				chromedp.OuterHTML("html", &html, chromedp.ByQuery),
			); err != nil {
				results <- &BrowserJobResult{
					Job: job,
					Err: err,
				}
			}

			results <- &BrowserJobResult{
				Job:  job,
				HTML: html,
			}
		}
	}()
}

func main() {
	var err error
	f := os.Stdin

	if len(os.Args) > 1 {
		f, err = os.Open(os.Args[1])
		check("opening file", err)
		defer f.Close()
	}

	items, err := LoadItems(f)
	check("reading input", err)

	err = LookupVendorPrices(items)
	check("looking up prices", err)

	// Filter for the vendor with the lowest cost for each item.
	items = ItemsByLowestCostVendor(items)

	totalPrice := 0
	totalShipping := 0

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintln(tw, "Description\tQuantity\tVendor\tPrice\tShipping\tURL")
	for _, item := range items {
		for _, vendItem := range item.VendorItems {
			price := vendItem.Price
			if price == -1 {
				fmt.Fprintf(tw, "%s\t%d\t%s\t?.??\t?.??\t%s\n", item.Description, item.Quantity, vendItem.VendorName, vendItem.URL)
				continue
			}

			fmt.Fprintf(tw, "%s\t%d\t%s\t%0.2f\t%0.2f\t%s\n", item.Description, item.Quantity, vendItem.VendorName, float64(vendItem.Price/100.0), float64(vendItem.Shipping/100.0), vendItem.URL)

			totalPrice += (vendItem.Price * item.Quantity)
			if vendItem.Shipping > -1 {
				totalShipping += (vendItem.Shipping * item.Quantity)
			}
		}
	}

	fmt.Fprintf(tw, "Total\t \t \t%0.2f\t%0.2f\t \n", float64(totalPrice/100.0), float64(totalShipping/100.0))
	fmt.Fprintf(tw, "Grand Total\t \t \t%0.2f\t \t \n", float64((totalPrice+totalShipping)/100.0))

	tw.Flush()
}

func ItemsByPreferredVendors(items []*Item, preferredVendors []string) map[string][]*Item {
	itemsByVendor := map[string][]*Item{}

	for _, item := range items {
		for _, mi := range item.VendorItems {
			vendorItems := itemsByVendor[mi.VendorName]
			if vendorItems == nil {
				vendorItems = []*Item{}
				itemsByVendor[mi.VendorName] = vendorItems
			}

			vendorItems = append(vendorItems, item)
		}
	}

	return itemsByVendor
}

func ItemsByLowestCostVendor(items []*Item) []*Item {
	itemsByCost := []*Item{}

	for _, item := range items {
		lowestCostItem := item.VendorItems[0]
		for _, vi := range item.VendorItems {
			viTotal := vi.TotalCost()
			if viTotal > -1 && viTotal < lowestCostItem.TotalCost() || lowestCostItem.TotalCost() == -1 && viTotal > -1 {
				lowestCostItem = vi
			}
		}

		// Create a new item with only one vendor item that is for the vendor
		// with the lowest cost.
		newItem := NewItem(item.Description, item.Quantity)
		newItem.VendorItems = []*VendorItem{lowestCostItem}

		itemsByCost = append(itemsByCost, newItem)
	}

	return itemsByCost
}

func check(msg string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s: %s", msg, err)
		os.Exit(1)
	}
}
