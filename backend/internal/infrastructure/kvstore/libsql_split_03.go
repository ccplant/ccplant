package kvstore

import (
	"encoding/json"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // register the sqlite driver for local file:// databases
)

func writeLibSQLLabelMatch(statement *strings.Builder, args *[]any, table, key string, values []string, negate bool) {
	// Kubernetes label keys cannot contain quotes. Keeping the JSON expression
	// literal (rather than binding the path) lets SQLite match expression indexes.
	escapedKey := strings.ReplaceAll(key, `"`, `\"`)
	if negate {
		statement.WriteString(" AND NOT")
	} else {
		statement.WriteString(" AND")
	}
	fmt.Fprintf(statement, ` json_extract(%s.metadata, '$.labels."%s"') IN (%s)`, table, escapedKey, strings.TrimSuffix(strings.Repeat("?,", len(values)), ","))
	for _, value := range values {
		*args = append(*args, value)
	}
}

func writeLibSQLLabelExists(statement *strings.Builder, args *[]any, table, alias, key string, values []string, negate bool) {
	if negate {
		statement.WriteString(" AND NOT")
	} else {
		statement.WriteString(" AND")
	}
	fmt.Fprintf(statement, " EXISTS (SELECT 1 FROM json_each(%s.metadata, '$.labels') AS %s WHERE %s.key = ?", table, alias, alias)
	*args = append(*args, key)
	if len(values) > 0 {
		fmt.Fprintf(statement, " AND %s.value IN (%s)", alias, strings.TrimSuffix(strings.Repeat("?,", len(values)), ","))
		for _, value := range values {
			*args = append(*args, value)
		}
	}
	statement.WriteString(")")
}

const recordMetadataFormat = "agentapi-kv-metadata/v1"

type recordMetadata struct {
	Format string            `json:"format"`
	Labels map[string]string `json:"labels"`
}

func marshalRecordMetadata(recordLabels map[string]string) ([]byte, error) {
	if recordLabels == nil {
		recordLabels = map[string]string{}
	}
	metadata, err := json.Marshal(recordMetadata{Format: recordMetadataFormat, Labels: recordLabels})
	if err != nil {
		return nil, fmt.Errorf("encode libSQL metadata: %w", err)
	}
	return metadata, nil
}

func unmarshalRecordMetadata(data []byte) (map[string]string, error) {
	var metadata recordMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	if metadata.Format != recordMetadataFormat {
		return nil, fmt.Errorf("unsupported metadata format %q", metadata.Format)
	}
	if metadata.Labels == nil {
		metadata.Labels = map[string]string{}
	}
	return metadata.Labels, nil
}
