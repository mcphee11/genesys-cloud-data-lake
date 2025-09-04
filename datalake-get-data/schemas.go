package start

import (
	"time"
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
