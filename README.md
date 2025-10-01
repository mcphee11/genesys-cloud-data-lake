# genesys-cloud-data-lake

An example of exporting analytics data into Google Cloud Big Query for historical reporting usage. For this example I will be using Google Cloud services pricing is dependent on that provider so be aware of their costs and data usage tiers etc. This example uses the new [Data Lake Extraction](https://developer.genesys.cloud/analyticsdatamanagement/analytics/extraction/) capability from Genesys Cloud.

`This example requires you to already have experience with both Google Cloud, Genesys Cloud & Golang programming language`

I have designed this in a way where it will GET all the different Meta data schema types even as new ones are added and dynamically add them into a new table into Google BigQuery. Without the need to change a single line of code in the function. To then use this data in a report you would still need to create a connection to this new table. Tables will be created with the schema type as the name.

## Step 1 - Create OAuth

The first thing you need to do is to create a `client credentials` OAuth token so that the code can access the Genesys Cloud API's to retrieve the data.

![](/images/oauth.png?raw=true)

For the `permissions` required I have broken them down into both Permissions as well as Scopes.

### Permissions:
```
analytics:datawarehouse:view
```
### Scopes:
```
analytics
analytics:readonly
```

Ensure you copy the details once saved as you will need these later on.

```
clientId
client secret
```

## Step 2 - GCP Project

Ensure you have a Google Cloud Project you plan to use for this that has a billing account linked and enabled Cloud Functions, BigQuery, Storage, Pub/Sub & Arc. You will also need to have the [GCloud CLI](https://cloud.google.com/sdk/docs/install) installed and setup to run the install commands. If any of these are NOT installed or setup correctly you will see errors from the CLI stating so and how to fix them including links to the Google documentation.

Also make sure you have [Golang](https://go.dev/doc/install) installed and setup as well, version 1.25+.

- Create a GCP `Bucket` created under your project and copy down the name of this bucket as you will need it later. In my example I'm using the bucket name `genesys_bigquery_datalake`.

- Create a BigQuery `Dataset` and copy down that name as well. In my example I'm using the data set name `genesys_bigquery_datalake`

- Also create a Pub/Sub Topic called `datalake-trigger` with a default schema.

![](/images/topic.png?raw=true)

## Step 3 - Deploy Cloud Functions

There is one Google Cloud function that needs to be deployed. This is located in this repository in a separate folder. To make my life easier I create files called deploy.sh run.sh & trigger.sh. These will not appear in the repository as they are part of the `.gitignore` as they include ids. You can run these function locally before deploying them to the cloud if you wish to test locally first.

As I'm based in Australia I am deploying the function into the `australia-southeast1` region. You can change this to be a different region based on where you are.

### datalake-get-data

Before deploying the function ensure you have done the above steps including creating the `Topic` in Pub/Sub.

Ensure you are inside the ./datalake-get-data Dir then run the below command with updating the parameters to be based on your environment. `NOTE:` I have set this to 4GiB memory depending on your file sizes you can configure this, you will see in the logs if you need more.

```
gcloud functions deploy datalake-get-data --gen2 --runtime=go125 --region=australia-southeast1 --source=. --entry-point=datalakeStart --trigger-topic=datalake-trigger --memory=4GiB --set-env-vars REGION=YOUR_REGION,CLIENT_ID=YOUR_CLIENTID,SECRET=YOUR_SECRET,BUCKETNAME=YOUR_BUCKET_NAME,PROJECTID=YOUR_PROJECTID,DATASETID=YOUR_DATASETID
```

For example my `REGION` is `mypurecloud.com.au` as im using an ORG in the APSE2 region of AWS.

This will deploy the gen2 cloud function to Google Cloud.

## Step 4 - Time based trigger

Now that all the pieces are in place you just need to create a Schedule to trigger the first function based on the time. In my case I have it trigger at Midnight each night to GET the data and populate the tables.

Create a Schedual called `datalake-trigger-time`, in my case I have made it at 00:00 each night this uses the `unix-cron` [format](https://cloud.google.com/scheduler/docs/configuring/cron-job-schedules).

```
0 0 * * *
```

![](/images/schedual.png?raw=true)

Ensure that the execution is then pointing to the Topic that you created earlier.

Once saved you can "force run" the schedule to test if its working. After this it will trigger the function at midnight which will put the files into the `Bucket` and once the files are uploaded in the correct folders it will then copy them to BigQuery in the tables based on the schema names. By default this will then delete the files and folders from the bucket to save on having them stored twice. If you wish you can remove this from the code to retain them in the bucket. If you do this you will probably want to make the uploads go into a date folder so you can disguise which files to them copy etc.

## Step 5 - Review the data

Open Up BiqQuery and look inside the data set that you created to see if the data has been put into the tables. Depending on the data received you will most likely have several tables created for each data schema type.

When you look at the cloud function logs depending on the amount of data you are requesting you may want to add either more RAM or CPU's to the function. As I have built it in Go with go routines this will support more CPU's if added to the configuration.

### Example of not enough RAM:
![](/images/error.png?raw=true)

### What the end to end logging looks like:
![](/images/logging.png?raw=true)

# Step 6 - Building the Report

Open up Google [Looker Studio](https://lookerstudio.google.com/navigation/reporting) Create a new `Blank Report` then add a Data Source. By default a new report should open up the default selection options including `BigQuery` where we have already put out data.

If your using Google Looker Studio as I am you will want to create a `Add calculated field` in your local timezone to then filter the report based on a DATETIME not just the INT64 UNIX time that comes from the export directly. I did debate doing this at the Database level but then you cant dynamiclly add additional timezones as well as some admins may be in different locations. So instead you can do this on the BI side easy enough.

When you add the calculated field you will need to use this formula:

```
DATETIME_ADD(PARSE_DATETIME("%s", CAST(FLOOR(conversationStart/1000) AS TEXT)), INTERVAL DATETIME_DIFF(CURRENT_DATETIME("Australia/Melbourne"), CURRENT_DATETIME("UTC"), HOUR) HOUR)
```

Simply replace the time zone with the one you require based on [IANA](https://www.iana.org/time-zones) formatting. You can then filter the reports in your own local timezone of choice based on that new metric.

NOTE: the above is based on the "conversations_parquet" table if you want to use it on a different table you will need to adjust the column value to suit that table.


Have fun building custom reports now!
