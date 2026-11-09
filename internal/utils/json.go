package utils

import (
	"encoding/json"
	"fmt"
	"strings"
)

func StructToTemplateData(aStruct any) (string, error) {
	data, err := json.Marshal(aStruct)
	if err != nil {
		return "", err
	}

	return strings.Trim(fmt.Sprintf("%q", string(data)), "\""), nil
}
