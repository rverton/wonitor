package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/gosimple/slug"
	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/urfave/cli/v2"
)

var db *bolt.DB

const TIMEOUT = 8 * time.Second
const BUCKET_URLS = "urls"

func addUrl(c *cli.Context) error {
	url := c.String("url")

	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BUCKET_URLS))
		return b.Put([]byte(url), []byte(""))
	})
}

func removeUrl(c *cli.Context) error {
	url := c.String("url")

	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BUCKET_URLS))
		return b.Delete([]byte(url))
	})
}

func list(c *cli.Context) error {
	return db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BUCKET_URLS))
		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			fmt.Printf("%v, %vB\n", string(k), len(v))
		}

		return nil
	})
}

func getUrl(c *cli.Context) error {
	url := c.String("url")

	return db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BUCKET_URLS))
		body := b.Get([]byte(url))

		if body == nil {
			fmt.Fprintln(os.Stderr, "url not found in store")
			return nil
		}

		if len(body) == 0 {
			fmt.Fprintln(os.Stderr, "<empty>")
			return nil
		}

		fmt.Println(string(body))

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

func handleDiff(url, bodyOld, bodyNew, outDir string, wg *sync.WaitGroup) {
	defer wg.Done()

	diffLen := Abs(len(bodyOld) - len(bodyNew))
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(bodyOld, bodyNew, false)

	if outDir == "" {
		fmt.Printf("%v, %vb:\n", url, diffLen)
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

func monitor(c *cli.Context) error {
	var wg sync.WaitGroup

	save := c.Bool("save")
	outDir := c.String("outDir")

	return db.Update(func(tx *bolt.Tx) error {

		b := tx.Bucket([]byte(BUCKET_URLS))
		c := b.Cursor()

		for url, bodyOld := c.First(); url != nil; url, bodyOld = c.Next() {
			u := string(url)

			resp, err := retrieve(u)
			if err != nil {
				log.Printf("err retrieving %v: %v", u, err)
				continue
			}

			bodyNew, err := ioutil.ReadAll(resp.Body)
			resp.Body.Close()

			if bytes.Compare(bodyOld, bodyNew) != 0 {

				wg.Add(1)
				go handleDiff(u, string(bodyOld), string(bodyNew), outDir, &wg)

				if save && b.Put(url, bodyNew) != nil {
					log.Printf("err updating body for %v: %v", u, err)
				}
			}

		}

		wg.Wait()

		return nil
	})
}

func initDb() error {
	return db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(BUCKET_URLS))
		return err
	})
}

func main() {
	var err error

	db, err = bolt.Open("my.db", 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := initDb(); err != nil {
		log.Fatal(err)
	}

	app := &cli.App{
		Name:  "wonitor",
		Usage: "web monitor",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "db",
				Usage: "database file",
				Value: "my.db",
			},
		},
		Commands: []*cli.Command{
			{
				Name:    "add",
				Aliases: []string{"a"},
				Usage:   "add endpoint to monitor",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "url",
						Usage:    "url to add",
						Required: true,
					},
				},
				Action: addUrl,
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
				Action: removeUrl,
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
				Action: getUrl,
			},
			{
				Name:    "list",
				Aliases: []string{"l"},
				Usage:   "list all monitored endpoints and their body size in bytes",
				Action:  list,
			},
			{
				Name:    "monitor",
				Aliases: []string{"m"},
				Usage:   "retrieve all urls and compare them",
				Action:  monitor,
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
				},
			},
		},
	}

	err = app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
