package collector

import (
	"encoding/json"
	"strconv"
)

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func parseInt(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
