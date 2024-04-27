## Introduction
**passwall-geosite-pt** is a utility designed to extract tracker domain names from torrents within qBittorrent and Transmission, and generate geosite files compatible with Xray (Passwall). This allows for the easy configuration of direct connections to trackers in Passwall rules. For qBittorrent, only the HTTP and HTTPS trackers of private seeds are retrieved. Due to API constraints in Transmission, all HTTP and HTTPS trackers, including those from BT seeds, are included. 

## Usage
```
./passwall-geosite-pt -qb admin:adminadmin@192.168.1.1:8080 -tr user:password@192.168.1.1:9091
  -dat string
    	The path where the .dat file will be written (default "/usr/share/v2ray/pt.dat")
  -qb value
    	qBittorrent API credentials and URL (e.g., admin:adminadmin@192.168.1.1:8080)
  -tr value
    	Transmission RPC credentials and URL (e.g., user:password@192.168.1.1:9091)
```

## Reference from PassWall rules
```
ext:pt.dat:tracker
```

Changed from https://github.com/gamesofts/v2ray-custom-geo

Build your own binary https://gitpod.io/#github.com/puteulanus/passwall-geosite-pt
