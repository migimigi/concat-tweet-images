package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"errors"

	"compress/zlib"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/sync/errgroup"
)

func main() {
	http.HandleFunc("/", handler)
	http.ListenAndServe("localhost:8080", nil)
}

func handler(w http.ResponseWriter, req *http.Request) {
	url, err := url.Parse(req.FormValue("q"))
	if err != nil {
		fmt.Println(err)
		return
	}
	isHorizon := req.FormValue("horizon") != ""
	result, err := appendImage(url.String(), isHorizon)
	if err != nil {
		fmt.Println(err)
		return
	}
	w.Header().Add("content-type", "image/jpeg")
	w.Write(result)
}

func appendImage(url string, isHorizon bool) ([]byte, error) {
	urls, err := parse(url)
	if err != nil {
		return nil, err
	}
	tmpdir, err := ioutil.TempDir("", "appendImage")
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

func parse(url string) ([]string, error) {
	fmt.Printf("parse: %s\n", url)
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
		} else {
			reader = readerZ
		}
		defer readerZ.Close()
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
		return nil, errors.New("can not parse")
	}
	return urls, nil
}

func download(urls []string, dir string) error {
	var eg errgroup.Group
	for i, u := range urls {
		index := i
		url := u
		eg.Go(func() error {
			fmt.Printf("start: %s %d\n", url, index)
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
			fmt.Printf("end: %s %d\n", url, index)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	return nil
}
