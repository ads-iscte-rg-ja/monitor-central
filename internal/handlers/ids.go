package handlers

import (
	"fmt"
	"strconv"
)

func requiredID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid positive numeric ID")
	}
	return id, nil
}

func optionalID(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	return requiredID(value)
}
