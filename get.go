package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func Get(u string) (io.ReadCloser, error) {
	u2 := strings.TrimPrefix(u, "http://")
	u2 = strings.TrimPrefix(u2, "https://")
	u2 = strings.Replace(u2, ".", "-", -1)
	u2 = strings.Replace(u2, `/`, "-", -1)
	u2 = strings.Replace(u2, `?`, "q", -1)
	u2 = strings.Replace(u2, `&`, "a", -1)
	u2 = strings.Replace(u2, `=`, "e", -1)
	fname := filepath.Join("cache", u2)
	if _, err := os.Stat(fname); os.IsNotExist(err) {
		fmt.Println(fname)
		resp, err := http.Get(u)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		dat, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if err = ioutil.WriteFile(fname, dat, 0644); err != nil {
			return nil, err
		}
		return ioutil.NopCloser(bytes.NewReader(dat)), nil
	}
	return os.Open(fname)
}
