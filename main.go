package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/urfave/cli"

	"compress/zlib"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/sync/errgroup"
)

func main() {
	app := cli.NewApp()

	app.Name = "concat-tweet-images"
	app.Version = "0.0.1"
	app.Commands = []cli.Command{
		{
			Name:  "server",
			Usage: "start server mode",
			Flags: []cli.Flag{
				cli.IntFlag{
					Name:  "port, p",
					Value: 8080,
					Usage: "specify a port number",
				},
			},
			Action: func(c *cli.Context) error {
				if err := startServer(c.Int("port")); err != nil {
					return cli.NewExitError(fmt.Sprintf("cannot start server: %v", err), 1)
				}
				return nil
			},
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func startServer(port int) error {
	if 1024 >= port && port <= 65535 {
		return fmt.Errorf("invalid port: %d", port)
	}
	http.HandleFunc("/", handler)
	err := http.ListenAndServe(fmt.Sprintf("localhost:%d", port), nil)
	if err != nil {
		return err
	}
	return nil
}

func handler(w http.ResponseWriter, req *http.Request) {
	isHorizon := req.FormValue("horizon") != ""
	result, err := concatImages(req.FormValue("q"), isHorizon)
	if err != nil {
		fmt.Println(err)
		return
	}
	w.Header().Add("content-type", "image/jpeg")
	w.Write(result)
}

func concatImages(url string, isHorizon bool) ([]byte, error) {
	urls, err := parse(url)
	if err != nil {
		return nil, err
	}
	tmpdir, err := ioutil.TempDir("", "concatImages")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpdir)
	err = download(urls, tmpdir)
	if err != nil {
		return nil, err
	}
	command := "-append"
	if isHorizon {
		command = "+append"
	}
	result, err := exec.Command("convert", command, filepath.Join(tmpdir, "/*"), "jpeg:-").Output()
	if err != nil {
		return nil, err
	}
	return result, nil
}

func parse(rawurl string) ([]string, error) {
	url, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	resp, err := http.Get(url.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "deflate" {
		readerZ, errZ := zlib.NewReader(resp.Body)
		if errZ != nil {
			return nil, errZ
		}
		defer readerZ.Close()
		reader = readerZ
	}
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return nil, err
	}
	urls := doc.Find("div.AdaptiveMedia").First().Find("div.AdaptiveMedia-photoContainer.js-adaptive-photo").Map(func(_ int, s *goquery.Selection) string {
		url, _ := s.Attr("data-image-url")
		return url
	})
	if len(urls) == 0 {
		return nil, fmt.Errorf("cannot find image: %s", url)
	}
	return urls, nil
}

func download(urls []string, dir string) error {
	var eg errgroup.Group
	for i, u := range urls {
		index := i
		url := u
		eg.Go(func() error {
			// Create the file
			fullpath := filepath.Join(dir, fmt.Sprintf("%d.jpg", index))
			out, err := os.Create(fullpath)
			if err != nil {
				return err
			}
			defer out.Close()

			// Get image
			resp, err := http.Get(url)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			// Writer the body to file
			_, err = io.Copy(out, resp.Body)
			if err != nil {
				return err
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	return nil
}
