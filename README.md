# Google Scraper

A fast Google Maps lead scraper written in Go. Type in something like `plumbers in Leicester` and it saves every business it finds with their name, address, phone number, website, rating, reviews and opening hours.

[![preview](assets/preview.gif)](assets/gscraper_vif.mp4)

It doesn't open a browser. It talks to the same endpoints the Google Maps website uses, with a Chrome TLS fingerprint, and fetches pages in parallel, so a few hundred businesses usually takes a few seconds.

## Usage

Download `gscraper.exe` from the releases page, or build it yourself:

```
go build -trimpath -o gscraper.exe ./cmd/gscraper
```

Run it with no arguments for the menu. Use the arrow keys and Enter to get around.

- **Search** one query
- **Batch search** a list of queries from a `.txt` file in `queries/`, or paste them in
- **Browse results** preview and open saved files
- **Proxies** load, test and clean up your proxy list
- **Settings** parallel requests, filters, columns, country

Results go in `results/` as CSV, Excel or JSON. [`london_test_example.csv`](results/london_test_example.csv) is a sample from a 100-query London test.

You can also run it straight from the command line:

```
gscraper -s "plumbers in Leicester" -t 50
gscraper -b queries/london_test.txt -t 0 -o results/london.xlsx
gscraper -s "dentists in Leeds" -proxies proxies.txt -c 48
```

`gscraper -h` lists every option.

## Proxies
[Best/My Proxy provider - $0.3/gb](https://nettify.xyz/auth/signup?ref=CD633C4A)

Put them in `proxies.txt`, one per line. Any of these formats work:

```
host:port
host:port:user:pass
user:pass@host:port
http://host:port:user:pass
http://user:pass@host:port
socks5://host:port:user:pass
socks5://user:pass@host:port
```

Every proxy is checked before a search starts. Dead ones are skipped, and so is any proxy that would leak your real IP. Turn on "Only scrape through proxies" in settings if you never want it to connect directly.

## Notes

Google stops returning results after roughly 120 to 170 per search, so for big areas split them into towns or neighbourhoods and use batch search.
