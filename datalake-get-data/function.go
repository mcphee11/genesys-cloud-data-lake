// Package start: Function to GET Genesys Cloud data from datalake API
// Stores the data is GCP Bucket
package start

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/storage"
	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/mypurecloud/platform-client-sdk-go/v129/platformclientv2"
	"google.golang.org/api/iterator"
)

func init() {
	functions.CloudEvent("datalakeStart", datalakeStart)
}

func datalakeStart(ctx context.Context, e event.Event) error {
	// Get and check for variables
	region := os.Getenv("REGION")
	if region == "" {
		return fmt.Errorf("REGION not set")
	}
	clientID := os.Getenv("CLIENT_ID")
	if clientID == "" {
		return fmt.Errorf("CLIENT_ID not set")
	}
	secret := os.Getenv("SECRET")
	if secret == "" {
		return fmt.Errorf("SECRET not set")
	}
	bucketName := os.Getenv("BUCKETNAME")
	if bucketName == "" {
		return fmt.Errorf("BUCKETNAME not set")
	}
	projectID := os.Getenv("PROJECTID")
	if projectID == "" {
		return fmt.Errorf("PROJECTID not set")
	}
	datasetID := os.Getenv("DATASETID")
	if datasetID == "" {
		return fmt.Errorf("DATASETID not set")
	}

	config := platformclientv2.GetDefaultConfiguration()
	config.BasePath = "https://api." + region
	err := config.AuthorizeClientCredentials(clientID, secret)
	if err != nil {
		return fmt.Errorf("logging in error: %v", err)
	}
	fmt.Println("Logged In to Genesys Cloud")

	// Get the date 1 day before
	yesterday := time.Now().AddDate(0, 0, -1)
	// Format the time in ISO 8601 format
	dateTime := yesterday.Format("2006-01-02T15:04:05.000Z")
	fmt.Printf("Interval Time: %v\n", dateTime)
	fileTypesFound := make(map[string]struct{})
	var uniqueTypesFound []string

	// Get the data
	metaData := getMetaData(*config, region, dateTime, "")
	// loop through each page as bulk API has limit of 200 files at a time
	for _, p := range metaData.Pages {
		var requestFiles BulkRequest
		for _, v := range p.Entities {
			requestFiles.Files = append(requestFiles.Files, v.ID)
		}
		fmt.Println("Got file Ids")
		bulkURLs := getBulkFiles(*config, requestFiles, region)

		var wg sync.WaitGroup
		totalStartTime := time.Now()
		fmt.Printf("Starting all %v page downloads concurrently...\n", len(bulkURLs.Entities))

		fileType := "NOT_FOUND"
		for _, item := range bulkURLs.Entities {
			// GET file name from data schema
			for _, d := range p.Entities {
				if d.ID == item.ID {
					fileType = d.DataSchema
				}
			}
			fileName := fmt.Sprintf("%s_%s.parquet", item.ID, fileType) // filepath.Base(item.ID)
			if _, ok := fileTypesFound[fileType]; !ok {
				fileTypesFound[fileType] = struct{}{}
				uniqueTypesFound = append(uniqueTypesFound, fileType)
			}
			wg.Add(1) // Increment the WaitGroup counter
			go downloadFile(item.SignedURL, fileName, bucketName, &wg)
		}
		wg.Wait() // Wait for all goroutines to finish
		totalDuration := time.Since(totalStartTime)
		fmt.Printf("Page downloads complete! Total time taken: %v\n", totalDuration)
	}

	// Upload each Dir to BigQuery
	var wgBq sync.WaitGroup
	totalStartTimeBq := time.Now()
	fmt.Println("Starting to upload all files to BigQuery")
	for _, typ := range uniqueTypesFound {
		fmt.Printf("Importing %s files to BigQuery...\n", typ)
		wgBq.Add(1)
		go importParquetFiles(projectID, datasetID, fmt.Sprintf("%s_parquet", typ), fmt.Sprintf("gs://%s/%s_parquet/*.parquet", bucketName, typ), &wgBq)
	}
	wgBq.Wait()
	totalDuration := time.Since(totalStartTimeBq)
	fmt.Printf("Totally finished uploading to BigQuery in %s YAY :) \n", totalDuration)

	// DELETE all files in the bucket to clean it out OPTIONAL...
	var wgDel sync.WaitGroup
	totalStartTimeDel := time.Now()
	for _, typ := range uniqueTypesFound {
		fmt.Printf("Deleting files in folder: %s_parquet\n", typ)
		wgDel.Add(1)
		go deleteFolder(ctx, bucketName, fmt.Sprintf("%s_parquet", typ), &wgDel)
	}
	wgDel.Wait()
	totalDurationDel := time.Since(totalStartTimeDel)
	fmt.Printf("Totally finished deleting files in %s\n", totalDurationDel)

	// Clean bigQuery tables and add date formatting...
	var wgSQL sync.WaitGroup
	totalStartTimeSQL := time.Now()
	for _, typ := range uniqueTypesFound {
		// Check for specific table and clean the data based on last 5000 rows in Db
		if typ == "conversations" {
			fmt.Printf("cleaning tables in: %s_parquet\n", typ)
			uuidString := "T2.conversationId = T.conversationId"
			wgSQL.Add(1)
			go cleanData(projectID, datasetID, fmt.Sprintf("%s_parquet", typ), uuidString, "updateTimestamp", &wgSQL)
		}
		if typ == "segments" {
			fmt.Printf("cleaning tables in: %s_parquet\n", typ)
			uuidString := "T2.conversationId = T.conversationId AND T2.segmentId = T.segmentId"
			wgSQL.Add(1)
			go cleanData(projectID, datasetID, fmt.Sprintf("%s_parquet", typ), uuidString, "updateTimestamp", &wgSQL)
		}
		if typ == "participant-attributes" {
			fmt.Printf("cleaning tables in: %s_parquet\n", typ)
			uuidString := "T2.conversationId = T.conversationId AND T2.participantId = T.participantId AND T2.attributeName = T.attributeName"
			wgSQL.Add(1)
			go cleanData(projectID, datasetID, fmt.Sprintf("%s_parquet", typ), uuidString, "eventTime", &wgSQL)
		}
		if typ == "conversation-metrics" {
			fmt.Printf("cleaning tables in: %s_parquet\n", typ)
			uuidString := "T2.conversationId = T.conversationId AND T2.sessionId = T.sessionId AND T2.metricName = T.metricName"
			wgSQL.Add(1)
			go cleanData(projectID, datasetID, fmt.Sprintf("%s_parquet", typ), uuidString, "metricTimestamp", &wgSQL)
		}
		if typ == "flow-metrics" {
			fmt.Printf("cleaning tables in: %s_parquet\n", typ)
			uuidString := "T2.conversationId = T.conversationId AND T2.sessionId = T.sessionId AND T2.metricName = T.metricName"
			wgSQL.Add(1)
			go cleanData(projectID, datasetID, fmt.Sprintf("%s_parquet", typ), uuidString, "metricTimestamp", &wgSQL)
		}
		if typ == "user-presence" {
			fmt.Printf("cleaning tables in: %s_parquet\n", typ)
			uuidString := "T2.userId = T.userId AND T.endTimestamp IS NULL"
			wgSQL.Add(1)
			go cleanData(projectID, datasetID, fmt.Sprintf("%s_parquet", typ), uuidString, "startTimestamp", &wgSQL)
		}
		if typ == "agent-routing-status" {
			fmt.Printf("cleaning tables in: %s_parquet\n", typ)
			uuidString := "T2.userId = T.userId AND T.endTimestamp IS NULL"
			wgSQL.Add(1)
			go cleanData(projectID, datasetID, fmt.Sprintf("%s_parquet", typ), uuidString, "startTimestamp", &wgSQL)
		}
	}
	wgSQL.Wait()
	totalDurationSQL := time.Since(totalStartTimeSQL)
	fmt.Printf("Totally finished cleaning SQL tables in %s\n", totalDurationSQL)

	return nil
}

func getMetaData(config platformclientv2.Configuration, region, dateTime, nextURI string) MetaDataResponsesArray {
	var url string
	if nextURI == "" {
		url = fmt.Sprintf("https://api.%s/api/v2/analytics/dataextraction/downloads/metadata?dateStart=%s&pageSize=200", region, dateTime)
	} else {
		url = fmt.Sprintf("https://api.%s/%s&dateStart=%s&pageSize=200", region, nextURI, dateTime)
	}
	client := &http.Client{}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("Error creating GET request: %s", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", config.AccessToken))
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error making HTTP request: %s", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response Body: %s", err)
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Received non-OK HTTP status: %s", resp.Status)
		fmt.Printf("Received body: %s", body)
	}
	var currentPage MetaDataResponse
	var allResponses MetaDataResponsesArray
	err = json.Unmarshal(body, &currentPage)
	if err != nil {
		fmt.Printf("Error un-marshaling JSON: %s", err)
	}
	// page through nextUri if there
	if currentPage.NextURI != "" {
		fmt.Println("Getting another page...")
		nextPage := getMetaData(config, region, dateTime, currentPage.NextURI)
		allResponses.Pages = append(allResponses.Pages, nextPage.Pages...)
	}
	allResponses.Pages = append(allResponses.Pages, currentPage)
	return allResponses
}

func getBulkFiles(config platformclientv2.Configuration, files BulkRequest, region string) BulkResponse {
	url := fmt.Sprintf("https://api.%s/api/v2/analytics/dataextraction/downloads/bulk", region)
	client := &http.Client{}
	jsonData, err := json.Marshal(files)
	if err != nil {
		fmt.Printf("Error marshaling data: %s", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("Error creating POST request: %s", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", config.AccessToken))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error making HTTP request: %s", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response Body: %s", err)
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Received non-OK HTTP status: %s\n", resp.Status)
		fmt.Printf("Received body: %s\n", body)
	}
	var response BulkResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		fmt.Printf("Error un-marshaling JSON: %s", err)
	}
	return response
}

func downloadFile(url, fileName, bucketName string, wg *sync.WaitGroup) {
	defer wg.Done()
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error downloading %s: %v\n", url, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Bad status code for %s: %d\n", url, resp.StatusCode)
		return
	}
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading Body: %s", err)
		return
	}
	err = uploadToBucket(bucketName, fileName, bytes)
	if err != nil {
		fmt.Printf("Error uploading to bucket: %s", err)
	}
}

func uploadToBucket(bucketName string, objectName string, payload []byte) error {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	schemaType := strings.Split(objectName, "_")
	schemaFormatted := strings.ReplaceAll(schemaType[1], ".", "_")
	bucket := client.Bucket(bucketName)
	fullPath := fmt.Sprintf("%s/%s", schemaFormatted, objectName)
	obj := bucket.Object(fullPath)

	// write to bucket
	writer := obj.NewWriter(ctx)
	_, err = writer.Write(payload)
	if err != nil {
		fmt.Printf("Writing Failed: %v\n", objectName)
		return err
	}
	err = writer.Close()
	if err != nil {
		fmt.Printf("Closing error on %v\n", objectName)
		return err
	}
	return nil
}

func importParquetFiles(projectID, datasetID, tableID, gcsPath string, wg *sync.WaitGroup) error {
	defer wg.Done()
	ctx := context.Background()
	client, err := bigquery.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("bigquery.NewClient: %v", err)
	}
	defer client.Close()

	gcsRef := bigquery.NewGCSReference(gcsPath)
	gcsRef.SourceFormat = bigquery.Parquet

	loader := client.Dataset(datasetID).Table(tableID).LoaderFrom(gcsRef)
	loader.WriteDisposition = bigquery.WriteAppend

	job, err := loader.Run(ctx)
	if err != nil {
		return err
	}
	status, err := job.Wait(ctx)
	if err != nil {
		return err
	}

	if status.Err() != nil {
		return fmt.Errorf("job completed with errors: %v", status.Errors)
	}
	fmt.Println("Bulk load job completed successfully.")
	return nil
}

func deleteFolder(ctx context.Context, bucketName, folderPath string, wg *sync.WaitGroup) error {
	defer wg.Done()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("storage.NewClient: %w", err)
	}
	defer client.Close()
	if !strings.HasSuffix(folderPath, "/") {
		folderPath += "/"
	}
	bucket := client.Bucket(bucketName)
	// Create an object iterator to list all objects with the specified prefix.
	it := bucket.Objects(ctx, &storage.Query{
		Prefix: folderPath,
	})
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("Bucket(%q).Objects: %w", bucketName, err)
		}
		object := bucket.Object(attrs.Name)
		if err := object.Delete(ctx); err != nil {
			return fmt.Errorf("Object(%q).Delete: %w", attrs.Name, err)
		}
	}
	fmt.Printf("Folder %q and its contents deleted from bucket %q\n", folderPath, bucketName)
	return nil
}

func cleanData(projectID, dataSetID, tableID, uuidString, updateTimeStamp string, wg *sync.WaitGroup) error {
	defer wg.Done()
	// FULL NOTICE. I'm not a SQL developer the below was ill admit generated with help from AI. If there are improvments to this please do a PR'
	sqlQuery := fmt.Sprintln("\n" +
		"-- This query cleans the table by removing older duplicate rows (limited to the 5000 latest rows),\n" +
		"-- The BEGIN and END block treats all statements as a single, multi-statement script.\n" +
		"BEGIN \n" +
		" -- Part 1: Declare and set variables at the beginning of the script.\n" +
		" DECLARE ts_cutoff INT64 DEFAULT 0; -- New variable for the 5000-row cutoff timestamp\n" +
		"\n" +
		" -- Determine the timestamp cutoff for limiting the deduplication scan.\n" +
		" -- This finds the updateTimeStamp of the 5000th most recent row.\n" +
		" SET ts_cutoff = COALESCE(\n" +
		"(\n" +
		"   SELECT\n" +
		"     " + updateTimeStamp + "\n" +
		"   FROM\n" +
		"     `" + projectID + "." + dataSetID + "." + tableID + "`\n" +
		"   ORDER BY\n" +
		"     " + updateTimeStamp + " DESC\n" +
		"   LIMIT 1 OFFSET 4999\n" +
		"),\n" +
		"0\n" +
		" );\n" +
		"\n" +
		" -- Part 2: Remove duplicate rows, keeping only the latest version, LIMITED TO THE 5000 MOST RECENT ROWS.\n" +
		" -- This DELETE logic is now simplified to ensure only older duplicate rows are removed.\n" +
		" DELETE FROM `" + projectID + "." + dataSetID + "." + tableID + "` AS T\n" +
		" WHERE\n" +
		"   -- 1. Ensure we only consider rows within the most recent 5000 block for deletion.\n" +
		"   T." + updateTimeStamp + " >= ts_cutoff\n" +
		"   -- 2. Delete the row (T) if a newer row (T2) with the same conversationId exists\n" +
		"   --    within the same 5000-row block.\n" +
		"   AND EXISTS (\n" +
		"     SELECT 1\n" +
		"     FROM `" + projectID + "." + dataSetID + "." + tableID + "` AS T2\n" +
		"     WHERE\n" +
		"       -- Match on one or more uuid\n" +
		"      " + uuidString + "\n" +
		"       -- T2 must have a strictly higher (newer) timestamp than T.\n" +
		"       AND T2." + updateTimeStamp + " > T." + updateTimeStamp + "\n" +
		"       -- T2 must also be within the 5000-row cutoff block (for safety/consistency)\n" +
		"       AND T2." + updateTimeStamp + " >= ts_cutoff\n" +
		"   );\n" +
		"\n" +
		"END;")

	ctx := context.Background()
	client, err := bigquery.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("bigquery.NewClient: %v", err)
	}
	defer client.Close()

	q := client.Query(sqlQuery)
	_, err = q.Read(ctx)
	if err != nil {
		fmt.Printf("Error cleaning via SQL: %s", err)
		return err
	}

	return nil
}
