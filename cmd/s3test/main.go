// cmd/s3test is a small smoke test for the S3 config in .env — uploads a
// tiny test object via storage.Service (the same code path the real server
// uses), then presigns and fetches it back to confirm both directions work.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pornlapatP/EV/internal/storage"
)

func main() {
	_ = godotenv.Load()
	ctx := context.Background()

	svc, err := storage.New(ctx)
	if err != nil {
		log.Fatal("storage.New failed: ", err)
	}

	key := "test/connectivity-check.txt"

	if _, err := svc.Upload(ctx, key, strings.NewReader("hello from s3test"), "text/plain"); err != nil {
		log.Fatal("upload failed: ", err)
	}
	fmt.Println("upload OK, key:", key)

	url, err := svc.PresignGet(ctx, key, storage.DefaultPresignTTL)
	if err != nil {
		log.Fatal("presign failed: ", err)
	}
	fmt.Println("presigned URL:", url)

	resp, err := http.Get(url)
	if err != nil {
		log.Fatal("fetch presigned URL failed: ", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("fetch OK: status=%d body=%q\n", resp.StatusCode, string(body))
}
