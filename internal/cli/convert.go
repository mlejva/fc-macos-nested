package cli

import "encoding/json"

// convertTo converts an interface{} value to a typed struct T via JSON marshaling.
// This is useful when working with the FirecrackerClientProvider interface which
// returns interface{} types.
func convertTo[T any](from interface{}) (*T, error) {
	data, err := json.Marshal(from)
	if err != nil {
		return nil, err
	}
	var to T
	if err := json.Unmarshal(data, &to); err != nil {
		return nil, err
	}
	return &to, nil
}
