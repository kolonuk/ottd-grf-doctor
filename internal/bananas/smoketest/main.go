// Throwaway manual smoke test for the bananas TCP client against the
// real content server. Not part of the build; run with `go run`.
package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kolonuk/ottd-grf-doctor/internal/bananas"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := &bananas.Client{}
	items, err := c.ListNewGRFs(ctx, 15*time.Second)
	if err != nil {
		fmt.Println("ListNewGRFs error:", err)
		return
	}
	fmt.Printf("Got %d NewGRF entries\n", len(items))

	var shark *bananas.ContentInfo
	for i := range items {
		if items[i].GRFIDHex() == "4A44BBB1" {
			shark = &items[i]
		}
	}
	if shark == nil {
		fmt.Println("SHARK not found in list")
		return
	}
	fmt.Printf("Found SHARK: contentID=%d name=%q version=%q filesize=%d md5=%x\n",
		shark.ContentID, shark.Name, shark.Version, shark.Filesize, shark.MD5)

	dctx, dcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer dcancel()
	files, err := c.Download(dctx, shark.ContentID, "/tmp/bananas-dl-test")
	if err != nil {
		fmt.Println("Download error:", err)
		return
	}
	fmt.Println("Extracted files:")
	for _, f := range files {
		fmt.Println(" ", f)
	}

	for _, f := range files {
		if filepath.Ext(f) == ".grf" {
			data, err := os.ReadFile(f)
			if err != nil {
				fmt.Println("read error:", err)
				continue
			}
			actual := md5.Sum(data)
			fmt.Printf("server-reported md5: %x\nactual file md5:     %x\nmatch: %v\n",
				shark.MD5, actual, shark.MD5 == actual)
		}
	}
}
