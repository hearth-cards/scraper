package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

var ch = make(chan *CardInfo)
var rCh = make(chan *CardInfo)
var wg sync.WaitGroup
var rWg sync.WaitGroup

func hpurl(path string) string {
	return "http://www.hearthpwn.com" + path
}

func main() {
	http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = 100
	flag.Parse()
	var url = hpurl("/cards?display=1&page=1")
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go worker()
	}
	rWg.Add(1)
	go collect()
	for {
		log.Println("!!!", url)
		resp, err := Get(url)
		if err != nil {
			log.Println("ERROR!", err)
			break
		}
		doc, err := goquery.NewDocumentFromReader(resp)
		if err != nil {
			log.Println("ERROR!", err)
			break
		}
		doc.Find(".listing tbody tr").Each(func(i int, s *goquery.Selection) {
			u, ok := s.Find(".manual-data-link").First().Attr("href")
			if ok {
				ci := &CardInfo{URL: hpurl(u)}
				ch <- ci
			}
		})
		nextButton := doc.Find(".paging-list .b-pagination-item a").Last()
		if nextButton.Text() != "Next" {
			break
		}
		resp.Close()
		url, _ = nextButton.Attr("href")
		url = hpurl(url)
	}
	close(ch)
	wg.Wait()
	close(rCh)
	rWg.Wait()
	serialize("cards.json")
	fmt.Println("DOWNLOADING")
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go downloader()
	}
	for _, c := range cards {
		if c.RegImage != "" {
			end := strings.TrimPrefix(c.RegImage, "http://media-Hearth.cursecdn.com/avatars/")
			end = strings.TrimPrefix(end, "https://media-Hearth.cursecdn.com/avatars/")
			end = strings.Replace(end, "/", "-", -1)
			if *dl {
				dlCh <- &DLReq{
					URL:  c.RegImage,
					Path: filepath.Join("..", "cardstatic", "img", end),
				}
			}
			c.RegImage = `https://cardstatic.hearth.cards/img/` + end
		}
		if c.GoldImage != "" {
			end := strings.TrimPrefix(c.GoldImage, "http://media-Hearth.cursecdn.com/goldCards/")
			end = strings.TrimPrefix(end, "https://media-Hearth.cursecdn.com/goldCards/")
			end = strings.Replace(end, "/", "-", -1)
			dest := filepath.Join("..", "goldstatic", "img", end)
			if *dl {
				dlCh <- &DLReq{
					URL:  c.GoldImage,
					Path: dest,
				}
			}
			c.GoldImage = `https://goldstatic.hearth.cards/img/` + end
		}
		for _, s := range c.Sounds {
			dest := filepath.Join("..", "yoggstatic", "s", filepath.Base(s.URL))
			if *dl {
				dlCh <- &DLReq{
					URL:  s.URL,
					Path: dest,
				}
			}
			s.URL = `https://yoggstatic.hearth.cards/s/` + filepath.Base(s.URL)
		}
	}
	close(dlCh)
	wg.Wait()
	serialize("rewritten.json")
	minify()
	serialize("../mrrgll/src/cards.ts")
}

func serialize(f string) {
	sort.Slice(cards, func(i, j int) bool {
		return cards[i].URL < cards[j].URL
	})
	out, err := os.Create(f)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	var dat []byte
	if strings.HasSuffix(f, ".ts") {
		out.WriteString(`import {CardDef} from './models'
			var Cards = `)
		dat, _ = json.Marshal(cards)
	} else {
		dat, _ = json.MarshalIndent(cards, "", "  ")
	}
	out.WriteString(strings.TrimSpace(string(dat)))
	if strings.HasSuffix(f, ".ts") {
		out.WriteString(` as CardDef[];
			export default Cards;`)
	}
}

type DLReq struct {
	URL  string
	Path string
}

var dlCh = make(chan *DLReq)
var dl = flag.Bool("dl", false, "download resources to appropriate folders")

type CardInfo struct {
	URL         string `json:",omitempty"`
	Name        string
	RegImage    string
	GoldImage   string `json:",omitempty"`
	Collectible bool   `json:",omitempty"`
	Sounds      []*SoundInfo
}

type SoundInfo struct {
	Name string
	URL  string
}

func (c *CardInfo) CleanupSounds() {
	for _, s := range c.Sounds {
		s.Name = strings.TrimPrefix(s.Name, "sound")
	}
	sort.Slice(c.Sounds, func(i, j int) bool {
		return c.Sounds[i].Name < c.Sounds[j].Name
	})
}

func minify() {
	// make smaller to go better in ts
	for _, c := range cards {
		c.URL = ""
		c.RegImage = strings.TrimPrefix(c.RegImage, "https://cardstatic.hearth.cards/img")
		c.GoldImage = strings.TrimPrefix(c.GoldImage, "https://goldstatic.hearth.cards/img")
		for _, s := range c.Sounds {
			s.URL = strings.TrimPrefix(s.URL, "https://yoggstatic.hearth.cards/s")
		}
	}
}

func worker() {
	defer wg.Done()
	for card := range ch {
		resp, err := Get(card.URL)
		if err != nil {
			log.Printf("Error on %s: %s", card.URL, err)
			continue
		}
		doc, err := goquery.NewDocumentFromReader(resp)
		if err != nil {
			log.Printf("Error on %s: %s", card.URL, err)
			resp.Close()
			continue
		}
		card.Name = doc.Find(".card-details>header>.caption").First().Text()
		//images
		reg := doc.Find(".u-typography-format .hscard-static")
		if reg.Length() > 0 {
			if i, ok := reg.First().Attr("src"); ok {
				card.RegImage = i
			}
		}
		gold := doc.Find(".hscard-video source").First()
		if gold.Length() > 0 {
			if i, ok := gold.First().Attr("src"); ok {
				card.GoldImage = i
			}
		}
		card.Sounds = []*SoundInfo{}
		doc.Find(".card-info p audio").Each(func(i int, s *goquery.Selection) {
			if sURL, ok := s.Attr("src"); ok {
				if id, ok := s.Attr("id"); ok {
					card.Sounds = append(card.Sounds, &SoundInfo{Name: id, URL: sURL})
				}
			}
		})
		// look for attributes
		doc.Find(".infobox ul li").Each(func(i int, s *goquery.Selection) {
			if s.Text() == "Collectible" {
				card.Collectible = true
			}
		})
		resp.Close()
		rCh <- card
	}
}

var cards = []*CardInfo{}

// read cards off appropriate channel and write them to disk when done
func collect() {
	odd, err := os.Create("oddities.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer rWg.Done()
	defer odd.Close()
	for card := range rCh {
		card.CleanupSounds()
		if card.RegImage == "" && card.GoldImage == "" {
			if len(card.Sounds) > 0 {
				fmt.Fprintf(odd, "%s (%s) %d sounds and no images???\n", card.Name, card.URL, len(card.Sounds))
				continue
			}
			fmt.Fprintf(odd, "%s (%s) has no imgs\n", card.Name, card.URL)
			continue
		}
		if card.RegImage == "" {
			fmt.Fprintf(odd, "%s (%s) has gold image, but no regular\n", card.Name, card.URL)
		}
		cards = append(cards, card)
	}

}

var seen = map[string]bool{}
var sLock sync.Mutex

func downloader() {
	defer wg.Done()
	for r := range dlCh {
		sLock.Lock()
		skip := seen[r.Path]
		if !skip {
			seen[r.Path] = true
		}
		sLock.Unlock()
		if skip {
			continue
		}
		if _, err := os.Stat(r.Path); !os.IsNotExist(err) {
			continue
		}
		resp, err := http.Get(r.URL)
		if err != nil {
			log.Printf("ERROR DOWNLOADING %s: %s", r.URL, err)
			continue
		}
		f, err := os.Create(r.Path)
		if err != nil {
			log.Printf("ERROR CREATING FILE %s: %s", r.Path, err)
			resp.Body.Close()
			continue
		}
		_, err = io.Copy(f, resp.Body)
		resp.Body.Close()
		f.Close()
		if err != nil {
			log.Printf("ERROR COPYING TO FILE %s: %s", r.Path, err)
			continue
		} else {
			fmt.Println(r.URL, r.Path)
		}
	}
}
