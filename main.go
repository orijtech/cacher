// Copyright 2018, Orijtech, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"

	"cloud.google.com/go/spanner"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/orijtech/tos3"
)

var spannerClient *spanner.Client
var spannerTableName = "cacher"
var s3Client *s3.S3

func init() {
	envCred := credentials.NewEnvCredentials()
	config := aws.NewConfig().WithCredentials(envCred)
	sess := session.Must(session.NewSession(config))
	s3Client = s3.New(sess)
}

func main() {
	var spannerName string
	var port int
	flag.StringVar(&spannerName, "spanner-name", "projects/census-demos/instances/census-demos/databases/demo1", "the cloud Spanner name")
	flag.IntVar(&port, "port", 9444, "the port to run the server on")
	flag.Parse()

	var err error
	var sessionPoolConfig = spanner.SessionPoolConfig{MinOpened: 5, WriteSessions: 1}
	ctx := context.Background()
	spannerClient, err = spanner.NewClientWithConfig(ctx, spannerName, spanner.ClientConfig{
		SessionPoolConfig: sessionPoolConfig,
	})
	if err != nil {
		log.Fatalf("Creating spanner.Client: %v", err)
	}

	addr := fmt.Sprintf(":%d", port)
	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(retrieveOrFetchAndCache))
	log.Printf("Serving at address %q", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("ListenAndServe err: %v", err)
	}
}

type Request struct {
	URL               string `json:"url"`
	ForceRefetch      bool   `json:"force_refetch"`
	ExpiryTimeSeconds int64  `json:"expiry_seconds"`
}

func retrieveOrFetchAndCache(w http.ResponseWriter, r *http.Request) {
	blob, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	req := new(Request)
	if err := json.Unmarshal(blob, req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	u, err := url.Parse(req.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	originalURL := u.String()
	// 1. Check the DB if it already exists
	// 1b: TODO: Take a lease so that no other
	//      concurrent process repeats this process.
	if record, err := checkDB(ctx, originalURL); err == nil && record != nil {
		replyAsJSON(ctx, w, record)
		return
	}

	// 2. Otherwise this was a cache miss. Time to download and save it to the CDN
	// TODO: Continue here
	h := md5.New()
	if _, err := io.WriteString(h, originalURL); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	md5Sum := fmt.Sprintf("%x", h.Sum(nil))
	s3URL, err := uploadToS3(originalURL, u.Host+"/"+md5Sum, md5Sum)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 3. Now update that record on Spanner
	record := &Record{Origin: originalURL, CachedURL: s3URL, TimeAt: now.Unix()}
	if err := saveRecordToSpanner(ctx, record); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if record, err := checkDB(ctx, originalURL); err == nil && record != nil {
		replyAsJSON(ctx, w, record)
		return
	}

	// By this point we failed to cache or retrieve the record.
	http.Error(w, "Failed to process record", http.StatusUnprocessableEntity)
}

const defaultS3Bucket = "cacher-app"

func uploadToS3(originalURL, s3Path, s3Name string) (string, error) {
	s3Req := &tos3.Request{
		Name:    s3Name,
		Path:    s3Path,
		URL:     originalURL,
		Private: false,

		S3Client: s3Client,
		// Now set the defaultBucket
		// TODO: Allow paying customers to set the path and bucket.
		Bucket: defaultS3Bucket,
	}

	res, err := s3Req.UploadToS3()
	if err != nil {
		return "", err
	}
	return res.URL, nil
}

type Record struct {
	Origin    string `json:"original_url,omitempty" spanner:"original_url,omitempty"`
	CachedURL string `json:"cached_url,omitempty" spanner:"cached_url,omitempty"`
	Err       string `json:"err,omitempty" spanner:"err,omitempty"`
	TimeAt    int64  `json:"time_at,omitempty" spanner:"time_at,omitempty"`
}

// saveRecordToSpanner saves the cached/CDN'd URL to Cloud Spanner returning an error if any.
func saveRecordToSpanner(ctx context.Context, record *Record) error {
	m, err := spanner.InsertStruct(spannerTableName, record)
	if err != nil {
		return err
	}
	_, err = spannerClient.Apply(ctx, []*spanner.Mutation{m})
	return err
}

func checkDB(ctx context.Context, originalURL string) (*Record, error) {
	row, err := spannerClient.Single().ReadRow(
		ctx, spannerTableName, spanner.Key{originalURL},
		[]string{"original_url", "cached_url", "err", "time_at"},
	)
	if err != nil {
		return nil, err
	}
	rc := new(Record)
	if err := row.ToStruct(rc); err != nil {
		return nil, err
	}
	return rc, nil
}

func replyAsJSON(ctx context.Context, w http.ResponseWriter, value interface{}) {
	enc := json.NewEncoder(w)
	_ = enc.Encode(value)
}
