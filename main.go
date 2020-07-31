package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	badger "github.com/dgraph-io/badger"

	"github.com/gosimple/slug"
	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/urfave/cli/v2"
)

const TIMEOUT = 8 * time.Second
const RESPONSE_BODY_LIMIT = 1024 * 1024 * 3 //3MB
const BUCKET_URLS = "urls"
const DB_FILE = "my.db"

// this headers will be included in the response which is stored
var headerToInclude = []string{
	"Host",
	"Content-Length",
	"Content-Type",
	"Location",
	"Access-Control-Allow-Origin",
	"Access-Control-Allow-Methods",
	"Access-Control-Expose-Headers",
	"Access-Control-Allow-Credentials",
	"Allow",
	"Content-Security-Policy",
	"Proxy-Authenticate",
	"Server",
	"WWW-Authenticate",
	"X-Frame-Options",
	"X-Powered-By",
}

func addUrl(db *badger.DB, url string, useStdin bool) error {
	return db.Update(func(tx *badger.Txn) error {

		if useStdin {

			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				url = scanner.Text()
				err := tx.Set([]byte(url), []byte(""))
				if err != nil {
					return err
				}

				fmt.Printf("+ %v\n", url)
			}

			return nil
		}
		fmt.Printf("+ %v\n", url)

		return tx.Set([]byte(url), []byte(""))
	})
}

func removeUrl(db *badger.DB, url string) error {
	return db.Update(func(tx *badger.Txn) error {
		return tx.Delete([]byte(url))
	})
}

func list(db *badger.DB) error {
	return db.View(func(tx *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 10
		it := tx.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			k := item.Key()
			err := item.Value(func(v []byte) error {
				fmt.Printf("%v, %vB\n", string(k), len(v))
				return nil
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
}

func getUrl(db *badger.DB, url string) error {
	return db.View(func(tx *badger.Txn) error {
		item, err := tx.Get([]byte(url))
		if err != nil {
			return err
		}

		valCopy, err := item.ValueCopy(nil)
		fmt.Println(string(valCopy))
		return nil
	})
}

func retrieve(url string) (*http.Response, error) {
	transport := &http.Transport{
		MaxIdleConnsPerHost: -1,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DisableKeepAlives: true,
	}

	var client = &http.Client{
		Timeout:   TIMEOUT,
		Transport: transport,
		CheckRedirect: func(redirectedRequest *http.Request, previousRequest []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return client.Get(url)
}

func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func handleDiff(url, bodyOld, bodyNew, outDir string) {
	diffLen := Abs(len(bodyOld) - len(bodyNew))
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(bodyOld, bodyNew, false)

	if outDir == "" {
		fmt.Printf("[%v] %vb diff:\n", url, diffLen)
		fmt.Println(dmp.DiffPrettyText(diffs))
	} else {
		filename := fmt.Sprintf("%v/%v_%v.html", outDir, time.Now().Format("20060201-150405"), slug.Make(url))
		data := []byte(dmp.DiffPrettyHtml(diffs))

		err := ioutil.WriteFile(filename, data, 0644)
		if err != nil {
			log.Printf("error saving output to %v: %v", filename, err)
		}
	}
}

// only leave a few interesting headers in the response
func minifyResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()

	var b bytes.Buffer

	b.WriteString(fmt.Sprintf("%v %v\n", resp.Proto, resp.Status))

	for _, header := range headerToInclude {
		v := resp.Header.Get(header)
		if v != "" {
			b.WriteString(fmt.Sprintf("%v: %v\n", header, v))
		}
	}

	limitedReader := &io.LimitedReader{R: resp.Body, N: RESPONSE_BODY_LIMIT}

	b.WriteString("\n")
	io.Copy(&b, limitedReader)

	return b.Bytes(), nil
}

func retrieveAndCompare(db *badger.DB, url, outDir string, save bool, bodyOld []byte, wg *sync.WaitGroup) {
	defer wg.Done()

	resp, err := retrieve(url)
	if err != nil {
		log.Printf("err retrieving %v: %v", url, err)
		return
	}

	bodyNew, err := minifyResponse(resp)
	if err != nil {
		log.Printf("err minifying resp: %v", err)
		return
	}

	if bytes.Compare(bodyOld, bodyNew) == 0 {
		return
	}

	handleDiff(url, string(bodyOld), string(bodyNew), outDir)

	if save {
		err := db.Update(func(tx *badger.Txn) error {
			return tx.Set([]byte(url), bodyNew)
		})

		if err != nil {
			log.Printf("err updating body: %v", err)
		}
	}
}

func monitor(db *badger.DB, save bool, outDir string, worker int) error {
	var wg sync.WaitGroup
	var sem = make(chan int, worker)

	err := db.Update(func(tx *badger.Txn) error {

		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 10
		it := tx.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			sem <- 1

			item := it.Item()
			url := string(item.Key())

			bodyOld, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			wg.Add(1)
			go func(db *badger.DB, url, outDIr string, save bool, bodyOld []byte, wg *sync.WaitGroup) {
				retrieveAndCompare(db, string(url), outDir, save, bodyOld, wg)
				<-sem
			}(db, string(url), outDir, save, bodyOld, &wg)
		}

		return nil
	})

	wg.Wait()

	return err
}

func initDb(path string) (*badger.DB, error) {
	options := badger.DefaultOptions(path)
	options.Logger = nil

	return badger.Open(options)
}

func main() {
	db, err := initDb(DB_FILE)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	app := &cli.App{
		Name:  "wonitor",
		Usage: "web monitor",
		Commands: []*cli.Command{
			{
				Name:    "add",
				Aliases: []string{"a"},
				Usage:   "add endpoint to monitor",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "url",
						Usage: "url to add",
					},
					&cli.BoolFlag{
						Name:  "stdin",
						Usage: "read urls from stdin, line by line",
					},
				},
				Action: func(c *cli.Context) error {
					if c.String("url") == "" && !c.Bool("stdin") {
						fmt.Println("please use --url or --stdin")
						os.Exit(1)
					}

					return addUrl(db, c.String("url"), c.Bool("stdin"))
				},
			},
			{
				Name:    "delete",
				Aliases: []string{"d"},
				Usage:   "deletes an endpoint",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "url",
						Usage:    "url to delete",
						Required: true,
					},
				},
				Action: func(c *cli.Context) error {
					return removeUrl(db, c.String("url"))
				},
			},
			{
				Name:    "get",
				Aliases: []string{"g"},
				Usage:   "get endpoint body",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "url",
						Usage:    "url to get from store",
						Required: true,
					},
				},
				Action: func(c *cli.Context) error {
					return getUrl(db, c.String("url"))
				},
			},
			{
				Name:    "list",
				Aliases: []string{"l"},
				Usage:   "list all monitored endpoints and their body size in bytes",
				Action: func(c *cli.Context) error {
					return list(db)
				},
			},
			{
				Name:    "monitor",
				Aliases: []string{"m"},
				Usage:   "retrieve all urls and compare them",
				Action: func(c *cli.Context) error {
					return monitor(db, c.Bool("save"), c.String("outDir"), c.Int("worker"))
				},
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "save",
						Usage: "save updates to store",
						Value: false,
					},
					&cli.StringFlag{
						Name:  "outDir",
						Usage: "save diffs as html to folder",
					},
					&cli.IntFlag{
						Name:  "worker",
						Usage: "numbers of worker to retrieve data",
						Value: 20,
					},
				},
			},
		},
	}

	err = app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
