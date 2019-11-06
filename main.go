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
	"strconv"
	"strings"

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
			Name:  "concat",
			Usage: "specify a tweet url",
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "horizon, H",
					Usage: "concat images horizontally",
				},
			},
			Action: func(c *cli.Context) error {
				if err := startConcat(c.Args(), c.Bool("horizon")); err != nil {
					return cli.NewExitError(fmt.Sprintf("cannot concat image: %v", err), 1)
				}
				return nil
			},
		},
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

func startConcat(args []string, isHorizon bool) error {
	if len(args) != 1 {
		return fmt.Errorf("require tweet url")
	}
	result, err := concatImages(args[0], isHorizon)
	if err != nil {
		return err
	}
	out, err := os.Create(fmt.Sprintf("%d.jpg", result.Tweetid))
	if err != nil {
		return err
	}
	_, err = out.Write(result.Image)
	err = out.Sync()
	err = out.Close()
	if err != nil {
		return err
	}
	return nil
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
	w.Header().Add("content-disposition", fmt.Sprintf("filename=\"%d.jpg\"", result.Tweetid))
	w.Write(result.Image)
}

type Tweet struct {
	Id  int64
	Url url.URL
}

func validateUrl(rawurl string) (*Tweet, error) {
	if rawurl == "" {
		return nil, fmt.Errorf("url is empty")
	}
	url, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	if url.Scheme != "https" || url.Host != "twitter.com" {
		return nil, fmt.Errorf("this url is not twitter: %s", url.String())
	}
	paths := strings.Split(url.Path, "/")
	if len(paths) != 4 || paths[2] != "status" {
		return nil, fmt.Errorf("this url is not tweet: %s", url.String())
	}
	tweetid, err := strconv.ParseInt(paths[3], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid tweet id: %s", url.String())
	}
	return &Tweet{Id: tweetid, Url: *url}, nil
}

type Result struct {
	Tweetid int64
	Image   []byte
}

func concatImages(rawurl string, isHorizon bool) (*Result, error) {
	tweet, err := validateUrl(rawurl)
	if err != nil {
		return nil, err
	}
	urls, err := parse(tweet.Url.String())
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
	bytes, err := exec.Command("convert", command, filepath.Join(tmpdir, "/*"), "jpeg:-").Output()
	if err != nil {
		return nil, err
	}
	return &Result{Image: bytes, Tweetid: tweet.Id}, nil
}

func parse(url string) ([]string, error) {
	resp, err := http.Get(url)
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
