package start

import (
	"time"

	"cloud.google.com/go/bigquery"
)

type MetaDataResponsesArray struct {
	Pages []MetaDataResponse `json:"pages"`
}

type MetaDataResponse struct {
	Entities           []MetaDataResponseEntity `json:"entities"`
	NextURI            string                   `json:"nextUri"`
	EnabledDataSchemas []string                 `json:"enabledDataSchemas"`
}

type MetaDataResponseEntity struct {
	ID          string    `json:"id"`
	DataSchema  string    `json:"dataSchema"`
	DateCreated time.Time `json:"dateCreated"`
	DateExpires time.Time `json:"dateExpires"`
}

type BulkRequest struct {
	Files []string `json:"files"`
}

type BulkResponse struct {
	Entities []BulkResponseEntity `json:"entities"`
}

type BulkResponseEntity struct {
	ID        string `json:"id"`
	SignedURL string `json:"signedUrl"`
}

func createConversationsParquetSchema() bigquery.Schema {
	conversationsParquetSchema := bigquery.Schema{
		{Name: "organizationId", Type: bigquery.StringFieldType},
		{Name: "conversationId", Type: bigquery.StringFieldType},
		{Name: "updateTimestamp", Type: bigquery.TimestampFieldType},
		{Name: "conversationStart", Type: bigquery.TimestampFieldType},
		{Name: "conversationEnd", Type: bigquery.TimestampFieldType},
		{Name: "conferenceStart", Type: bigquery.TimestampFieldType},
		{Name: "conversationInitiator", Type: bigquery.StringFieldType},
		{Name: "customerParticipation", Type: bigquery.BooleanFieldType},
		{Name: "divisionIds", Type: bigquery.StringFieldType, Repeated: true},
		{Name: "externalTag", Type: bigquery.StringFieldType},
		{Name: "knowledgeBaseIds", Type: bigquery.StringFieldType, Repeated: true},
		{Name: "mediaStatsMinConversationMos", Type: bigquery.FloatFieldType},
		{Name: "mediaStatsMinConversationRFactor", Type: bigquery.FloatFieldType},
		{Name: "originatingDirection", Type: bigquery.StringFieldType},
		{Name: "originatingSocialMediaPublic", Type: bigquery.BooleanFieldType},
		{Name: "selfServed", Type: bigquery.BooleanFieldType},
		{Name: "inactivityTimeout", Type: bigquery.TimestampFieldType},
	}
	return conversationsParquetSchema
}
