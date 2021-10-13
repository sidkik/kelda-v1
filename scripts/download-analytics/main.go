package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	log "github.com/sirupsen/logrus"

	"github.com/sidkik/kelda-v1/pkg/errors"
	// "github.com/sidkik/kelda-v1/scripts/make-license/config"
)

var csvHeader = []string{"customer", "time", "namespace", "event", "additional"}

type analyticsUpload struct {
	customer   string
	bucketName string
	objectKey  string
}

func main() {
	outPath := "combined-analytics.csv"
	// if err := downloadAnalytics(outPath); err != nil {
	// 	fmt.Fprintf(os.Stderr, "Failed to write analytics: %s", err)
	// }
}

// The analytics are spread out over multiple buckets, and multiple files
// within each bucket. Each customer has their own bucket, and new events are
// periodically uploaded as new keys to the bucket.
// This function finds all customer buckets, and appends all of the keys into
// one single csv.
// func downloadAnalytics(outPath string) error {
// 	sess, err := session.NewSession(&aws.Config{
// 		Region: aws.String(config.AWSRegion),
// 		Credentials: credentials.NewStaticCredentials(
// 			config.AWSAccessKeyID, config.AWSSecretAccessKey, ""),
// 	})
// 	if err != nil {
// 		return errors.WithContext(err, "create aws session")
// 	}

// 	out, err := os.Create(outPath)
// 	if err != nil {
// 		return errors.WithContext(err, "create output file")
// 	}
// 	defer out.Close()

// 	csvWriter := csv.NewWriter(out)
// 	defer csvWriter.Flush()
// 	if err := csvWriter.Write(csvHeader); err != nil {
// 		return errors.WithContext(err, "write csv header")
// 	}

// 	c := s3.New(sess)
// 	parts, err := getAnalyticsParts(c)
// 	if err != nil {
// 		return errors.WithContext(err, "get analytics references")
// 	}

// 	customersSet := map[string]struct{}{}
// 	for _, part := range parts {
// 		customersSet[part.customer] = struct{}{}
// 		if err := appendAnalytics(c, csvWriter, part); err != nil {
// 			return errors.WithContext(err, "write analytics contents")
// 		}
// 	}

// 	var customers []string
// 	for customer := range customersSet {
// 		customers = append(customers, customer)
// 	}
// 	log.WithField("customers", customers).Info("Successfully downloaded analytics")

// 	return nil
// }

// func getAnalyticsParts(c *s3.S3) ([]analyticsUpload, error) {
// 	// List all the buckets.
// 	listResp, err := c.ListBuckets(&s3.ListBucketsInput{})
// 	if err != nil {
// 		return nil, errors.WithContext(err, "list buckets")
// 	}

// 	// For each bucket, check whether it's an analytics bucket, and if so,
// 	// collect all of the keys within it.
// 	var parts []analyticsUpload
// 	for _, bucket := range listResp.Buckets {
// 		region, err := s3manager.GetBucketRegionWithClient(
// 			context.Background(), c, *bucket.Name)
// 		if err != nil {
// 			return nil, errors.WithContext(err, "get bucket region")
// 		}

// 		// The S3 code can only be run for buckets in the same region as the
// 		// client due to the way AWS handles permissions.
// 		// Because the license generator creates buckets based on
// 		// config.AWSRegion, it's safe to ignore buckets outside this region.
// 		if region != config.AWSRegion {
// 			continue
// 		}

// 		tagsResp, err := c.GetBucketTagging(&s3.GetBucketTaggingInput{
// 			Bucket: bucket.Name,
// 		})
// 		if err != nil {
// 			// If the error is because the bucket doesn't have any tags at all,
// 			// it's because it's not an analytics bucket, and the bucket should
// 			// just be skipped.
// 			if awsErr, ok := err.(awserr.Error); ok {
// 				if awsErr.Code() == "NoSuchTagSet" {
// 					continue
// 				}
// 			}
// 			return nil, errors.WithContext(err, "get bucket tags")
// 		}

// 		if !isAnalyticsBucket(tagsResp.TagSet) {
// 			continue
// 		}

// 		customer, ok := getCustomer(tagsResp.TagSet)
// 		if !ok {
// 			log.WithField("bucket", *bucket.Name).Warn("Skipping malformed bucket: no customer tag")
// 			continue
// 		}

// 		// Don't include the CI analytics in our data visualizations.
// 		if customer == "ci" {
// 			continue
// 		}

// 		keys, err := getKeysInBucket(c, *bucket.Name)
// 		if err != nil {
// 			return nil, errors.WithContext(err, "get objects")
// 		}

// 		for _, key := range keys {
// 			parts = append(parts, analyticsUpload{
// 				customer:   customer,
// 				bucketName: *bucket.Name,
// 				objectKey:  key,
// 			})
// 		}
// 	}
// 	return parts, nil
// }

// // getKeysInBucket returns all the keys in the given bucket.
// func getKeysInBucket(c *s3.S3, bucketName string) (keys []string, err error) {
// 	var marker *string
// 	for {
// 		listResp, err := c.ListObjects(&s3.ListObjectsInput{
// 			Bucket: &bucketName,
// 			Marker: marker,
// 		})
// 		if err != nil {
// 			return nil, errors.WithContext(err, "list bucket objects")
// 		}

// 		for _, object := range listResp.Contents {
// 			keys = append(keys, *object.Key)
// 		}

// 		if *listResp.IsTruncated {
// 			marker = listResp.NextMarker
// 		} else {
// 			return keys, nil
// 		}
// 	}
// }

// func appendAnalytics(c *s3.S3, out *csv.Writer, upload analyticsUpload) error {
// 	// Pull the analytics chunk from S3.
// 	objResp, err := c.GetObject(&s3.GetObjectInput{
// 		Bucket: &upload.bucketName,
// 		Key:    &upload.objectKey,
// 	})
// 	if err != nil {
// 		return errors.WithContext(err, "get")
// 	}

// 	// Parse the uploaded analytics.
// 	csvReader := csv.NewReader(objResp.Body)
// 	records, err := csvReader.ReadAll()
// 	if err != nil {
// 		return errors.WithContext(err, "parse csv")
// 	}

// 	// Modify the record to include the customer name, and write it to the
// 	// combined output.
// 	for _, record := range records {
// 		recordWithCustomer := append([]string{upload.customer}, record...)
// 		if err := out.Write(recordWithCustomer); err != nil {
// 			return errors.WithContext(err, "write")
// 		}
// 	}
// 	return nil
// }

// func isAnalyticsBucket(tags []*s3.Tag) bool {
// 	for _, tag := range tags {
// 		if *tag.Key == *config.AnalyticsTag.Key {
// 			return true
// 		}
// 	}
// 	return false
// }

// func getCustomer(tags []*s3.Tag) (string, bool) {
// 	for _, tag := range tags {
// 		if *tag.Key == config.CustomerTagKey {
// 			return *tag.Value, true
// 		}
// 	}
// 	return "", false
// }
