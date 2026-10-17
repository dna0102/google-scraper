package ui

import (
	"bufio"
	"os"
	"strings"
)

const byteOrderMark = "\ufeff"

func ReadQueries(path string) ([]string, error) {
	f, err := os.Open(strings.Trim(strings.TrimSpace(path), `"'`))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var queries []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		queries = addQuery(queries, sc.Text())
	}
	return queries, sc.Err()
}

func splitQueries(text string) []string {
	var queries []string
	for _, line := range strings.Split(text, "\n") {
		queries = addQuery(queries, line)
	}
	return queries
}

func addQuery(queries []string, line string) []string {
	line = strings.TrimSpace(strings.TrimPrefix(line, byteOrderMark))
	if line == "" || strings.HasPrefix(line, "#") {
		return queries
	}
	return append(queries, line)
}
