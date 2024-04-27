package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"bytes"
    "net"

	"github.com/gogo/protobuf/proto"
	"v2ray.com/core/app/router"
)

var (
	datPath = flag.String("dat", "/usr/share/v2ray/pt.dat", "The path where the .dat file will be written")
	qb      = newMultiString("qb", "qBittorrent API credentials and URL (e.g., admin:adminadmin@192.168.1.1:8080)")
	tr      = newMultiString("tr", "Transmission RPC credentials and URL (e.g., user:password@192.168.1.1:9091)")
)

type multiString []string

func (m *multiString) String() string {
	return fmt.Sprintf("%v", *m)
}

func (m *multiString) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func newMultiString(name string, usage string) *[]string {
	var s []string
	flag.Var((*multiString)(&s), name, usage)
	return &s
}

// Transmission-specific types
type Tracker struct {
	Announce string `json:"announce"`
	Url string `json:"url"`
}

type TorrentInfo struct {
	Hash string `json:"hash"`
}

type TorrentProperties struct {
	IsPrivate bool `json:"is_private"`
}

type Torrent struct {
	Trackers []Tracker `json:"trackers"`
}

type TorrentsFetchResponse struct {
	Arguments struct {
		Torrents []Torrent `json:"torrents"`
	} `json:"arguments"`
}

// Helper function to fetch session ID and retry request with session ID
func getSessionIDAndRetry(req *http.Request, client *http.Client) (*http.Response, error) {
    // 读取并保存原始 Body 数据
    bodyData, err := ioutil.ReadAll(req.Body)
    if err != nil {
        return nil, err
    }
    req.Body.Close() // 关闭原始 Body

    // 重新设置 Body
    req.Body = ioutil.NopCloser(bytes.NewReader(bodyData))

    // 发送请求
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }

    if resp.StatusCode == 409 {
        // 读取 session ID 并设置
        sessionID := resp.Header.Get("X-Transmission-Session-Id")
        req.Header.Set("X-Transmission-Session-Id", sessionID)
        resp.Body.Close() // 关闭前一个响应的 Body 以避免资源泄漏

        // 重置 Body 以重新发送请求
        req.Body = ioutil.NopCloser(bytes.NewReader(bodyData))

        // 再次尝试发送请求
        resp, err = client.Do(req)
        if err != nil {
            return nil, err
        }
    }

    return resp, err
}

func fetchDomainsFromTR(urlWithCred string) ([]*router.Domain, error) {
    parts := strings.Split(urlWithCred, "@")
    if len(parts) != 2 {
        return nil, fmt.Errorf("invalid transmission parameter format")
    }
    creds, baseURL := parts[0], "http://" + parts[1] + "/transmission/rpc"
    authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))

    client := &http.Client{}
    jsonData := `{"method":"torrent-get","arguments":{"fields":["id","name","trackers"]}}`
    req, err := http.NewRequest("POST", baseURL, strings.NewReader(jsonData))
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", authHeader)
    req.Header.Set("Content-Type", "application/json")
    //req.Header.Set("Content-Length", fmt.Sprintf("%d", len(jsonData)))

    resp, err := getSessionIDAndRetry(req, client)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("request failed with status: %s", resp.Status)
    }

    var response TorrentsFetchResponse
    if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
        return nil, err
    }

    domains := make([]*router.Domain, 0)

    for _, torrent := range response.Arguments.Torrents {
        for _, tracker := range torrent.Trackers {
            parsedUrl, err := url.Parse(tracker.Announce)
            // 确保解析无误且 URL 协议是以 "http" 开头，且主机名非空，且主机名不是 IP 地址
            if err != nil || 
               !strings.HasPrefix(parsedUrl.Scheme, "http") || 
               parsedUrl.Hostname() == "" || 
               net.ParseIP(parsedUrl.Hostname()) != nil {
                continue
            }
            domains = append(domains, &router.Domain{Type: router.Domain_Domain, Value: parsedUrl.Hostname()})
        }
    }

    return domains, nil
}

func authenticateAndFetchJSON(urlWithCred, path string, target interface{}) error {
	// Split the credentials and URL
	parts := strings.Split(urlWithCred, "@")
	if len(parts) != 2 {
		return fmt.Errorf("invalid parameter format")
	}
	creds, baseURL := parts[0], "http://"+parts[1]+"/api/v2"

	// Split username and password
	usernamePassword := strings.Split(creds, ":")
	if len(usernamePassword) != 2 {
		return fmt.Errorf("invalid credentials format")
	}

	// Setup HTTP client
	client := &http.Client{}
	resp, err := client.PostForm(baseURL+"/auth/login", url.Values{"username": {usernamePassword[0]}, "password": {usernamePassword[1]}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("authentication failed with status: %s", resp.Status)
	}

	cookie := ""
	for _, c := range resp.Cookies() {
		if c.Name == "SID" {
			cookie = c.Value
			break
		}
	}
	if cookie == "" {
		return fmt.Errorf("SID cookie not found")
	}

	// Fetch JSON data
	req, err := http.NewRequest("GET", baseURL+path, nil)
	if err != nil {
		return err
	}
	req.AddCookie(&http.Cookie{Name: "SID", Value: cookie})

	resp, err = client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(target)
}

func main() {
    flag.Parse()

    if len(*qb) == 0 && len(*tr) == 0 {
        fmt.Println("At least one qb or tr parameter must be specified.")
        flag.Usage()
        return
    }

    domainSet := make(map[string]bool)
    errorOccurred := false // Flag to indicate if an error has occurred

    // Process qBittorrent URLs
    for _, qbURL := range *qb {
        var torrents []TorrentInfo
        err := authenticateAndFetchJSON(qbURL, "/torrents/info", &torrents)
        if err != nil {
            fmt.Printf("Error fetching torrents from qBittorrent at %s: %v\n", qbURL, err)
            errorOccurred = true // Set error flag
            continue
        }

        for _, torrent := range torrents {
            var properties TorrentProperties
            err := authenticateAndFetchJSON(qbURL, fmt.Sprintf("/torrents/properties?hash=%s", torrent.Hash), &properties)
            if err != nil {
                continue
            }

            if properties.IsPrivate {
                var trackers []Tracker
                err := authenticateAndFetchJSON(qbURL, fmt.Sprintf("/torrents/trackers?hash=%s", torrent.Hash), &trackers)
                if err != nil {
                    continue
                }

                for _, tracker := range trackers {
                    u, err := url.Parse(tracker.Url)
                    if err != nil {
                        continue
                    }
                    if u.Hostname() != "" {
                        domainSet[u.Hostname()] = true
                    }
                }
            }
        }
    }

    // Process Transmission URLs
    for _, trURL := range *tr {
        domains, err := fetchDomainsFromTR(trURL)
        if err != nil {
            fmt.Printf("Error fetching domains from Transmission at %s: %v\n", trURL, err)
            errorOccurred = true // Set error flag
            continue
        }
        for _, domain := range domains {
            domainSet[domain.Value] = true
        }
    }

    // Check if an error occurred and skip writing the file
    if errorOccurred {
        fmt.Println("Errors occurred during domain fetching; file write skipped.")
        return
    }

    // Generate and save the GeoSiteList
    domains := make([]*router.Domain, 0, len(domainSet))
    for domain := range domainSet {
        domains = append(domains, &router.Domain{Type: router.Domain_Domain, Value: domain})
    }
    sort.Slice(domains, func(i, j int) bool { return domains[i].Value < domains[j].Value })

    geoSiteList := &router.GeoSiteList{Entry: []*router.GeoSite{{CountryCode: "TRACKER", Domain: domains}}}
    data, err := proto.Marshal(geoSiteList)
    if err != nil {
        fmt.Println("Failed to marshal GeoSiteList:", err)
        return
    }

    if err := ioutil.WriteFile(*datPath, data, 0666); err != nil {
        fmt.Println("Failed to write file:", err)
        return
    }

    fmt.Printf("tracker -> %s\n", *datPath)
    for _, domain := range domains {
        fmt.Println(domain.Value)
    }
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", "passwall-geosite-pt")
		fmt.Println("Example:")
		fmt.Println("./passwall-geosite-pt -qb admin:adminadmin@192.168.1.1:8080 -tr user:password@192.168.1.1:9091")
		flag.PrintDefaults()
	}
}
