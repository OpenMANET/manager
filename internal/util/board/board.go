package board

import (
	"encoding/json"
	"os"
)

// NewBoard reads the board configuration from "/etc/board.json",
// unmarshals the JSON data into a Board struct, and returns a pointer to it.
// Returns an error if the file cannot be read or the JSON is invalid.
func NewBoardConfigInfo() (*Board, error) {
	data, err := os.ReadFile("/etc/board.json")
	if err != nil {
		return nil, err
	}

	var board Board
	if err := json.Unmarshal(data, &board); err != nil {
		return nil, err
	}

	return &board, nil
}
