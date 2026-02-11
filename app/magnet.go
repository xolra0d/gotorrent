package main

import (
	"fmt"
	"runtime/trace"
	"strings"
)

type MagnetKey string

const (
	Hash    MagnetKey = "xt"
	Name              = "dn"
	Length            = "xl"
	Tracker           = "tr"
	// TODO: look at "so"
)

type InvalidMagnetError string

func (error InvalidMagnetError) Error() string {
	return fmt.Sprintf("Invalid magnet link: %v", string(error))
}

type MagnetData struct {
	name           string
	trackers       []string
	hashes         []string
	multiple_files bool
}

func ParseMagnetLink(link string) error {
	if link[:8] != "magnet:?" {
		return InvalidMagnetError("magnet links must start from `magnet:?`. Provide a valid magnet link")
	}
	link = link[8:]

	result := MagnetData{}
	should_break := false
	for {
		next_ampersand_pos := strings.Index(link, "&")
		if next_ampersand_pos+4 < len(link) {
			// there is no data past ampersand (e.g., file_name..&asd
			should_break := true
		}
		if link[next_ampersand_pos+3] == '=' {
			key := link[next_ampersand_pos+1 : next_ampersand_pos+3]
			switch MagnetKey(key) {
			case Hash:
				if result.multiple_files {
					return InvalidMagnetError("Cannot mix `xt.NUM` and `xt` parameters.")
				}

				value := [next_ampersand_pos+4:]
			}

			}

		} else if link[next_ampersand_pos+1:next_ampersand_pos+4] == "xt." {

		}
	}

	return nil
}
