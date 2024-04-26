package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"

	"github.com/gogo/protobuf/proto"
	"v2ray.com/core/app/router"
)

var (
	datPath = flag.String("dat", "/usr/share/v2ray/pt.dat", "The path where the .dat file will be written")
	apiBase = flag.String("api", "", "The base URL to the qBittorrent API (e.g., 192.168.1.1:8080)")
	username = flag.String("user", "admin", "The username for qBittorrent API")
	password = flag.String("pass", "adminadmin", "The password for qBittorrent API")
)

type TorrentInfo struct {
	Hash string `json:"hash"`
}

type Tracker struct {
	Url string `json:"url"`
}

type TorrentProperties struct {
	IsPrivate bool `json:"is_private"`
}

func authenticate(client *http.Client, baseURL string) (*http.Cookie, error) {
	resp, err := client.PostForm(baseURL+"/auth/login", url.Values{"username": {*username}, "password": {*password}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("authentication failed with status: %s", resp.Status)
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "SID" {
			return cookie, nil
		}
	}
	return nil, fmt.Errorf("SID cookie not found")
}

func fetchJSON(client *http.Client, endpoint string, target interface{}, cookie *http.Cookie, baseURL string) error {
	req, err := http.NewRequest("GET", baseURL+endpoint, nil)
	if err != nil {
		return err
	}
	req.AddCookie(cookie)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(target)
}

func main() {
	flag.Parse()

	if *apiBase == "" {
		fmt.Println("API base URL is required.")
		flag.Usage()
		return
	}

	client := &http.Client{}
	baseURL := "http://" + *apiBase + "/api/v2"

	// Authenticate
	cookie, err := authenticate(client, baseURL)
	if err != nil {
		fmt.Println("Error authenticating:", err)
		return
	}

	domainSet := make(map[string]bool)
	// Fetch torrents
	var torrents []TorrentInfo
	err = fetchJSON(client, "/torrents/info", &torrents, cookie, baseURL)
	if err != nil {
		fmt.Println("Error fetching torrents:", err)
		return
	}

	for _, torrent := range torrents {
		var properties TorrentProperties
		err := fetchJSON(client, fmt.Sprintf("/torrents/properties?hash=%s", torrent.Hash), &properties, cookie, baseURL)
		if err != nil {
			fmt.Println("Error fetching properties for torrent:", err)
			continue
		}

		if properties.IsPrivate {
			var trackers []Tracker
			err := fetchJSON(client, fmt.Sprintf("/torrents/trackers?hash=%s", torrent.Hash), &trackers, cookie, baseURL)
			if err != nil {
				fmt.Println("Error fetching trackers for torrent:", err)
				continue
			}

			for _, tracker := range trackers {
				u, err := url.Parse(tracker.Url)
				if err != nil {
					fmt.Println("Error parsing tracker URL:", err)
					continue
				}
				if u.Hostname() != "" {
					domainSet[u.Hostname()] = true
				}
			}
		}
	}

	domains := make([]*router.Domain, 0)
	for domain := range domainSet {
		domains = append(domains, &router.Domain{Type: router.Domain_Domain, Value: domain})
	}
	sort.Slice(domains, func(i, j int) bool {
		return domains[i].Value < domains[j].Value
	})

	// Create GeoSiteList and marshal to protobuf
	geoSiteList := &router.GeoSiteList{
		Entry: []*router.GeoSite{
			{
				CountryCode: "PT",
				Domain:      domains,
			},
		},
	}

	data, err := proto.Marshal(geoSiteList)
	if err != nil {
		fmt.Println("Failed to marshal GeoSiteList:", err)
		return
	}

	// Write to file
	err = ioutil.WriteFile(*datPath, data, 0666)
	if err != nil {
		fmt.Println("Failed to write file:", err)
		return
	}

	fmt.Printf("PT -> %s\n", *datPath)
	for _, domain := range domains {
		fmt.Println(domain.Value)
	}
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", "passwall-geosite-pt")
		fmt.Println("Example:")
		fmt.Println("./passwall-geosite-pt -api 192.168.1.1:8080 -user admin -pass adminadmin -dat /usr/share/v2ray/pt.dat")
		flag.PrintDefaults()
	}
}
