package main

import (
	"fmt"
	"net/url"
	"strings"
)

type MagnetKey string

const (
	Hash    MagnetKey = "xt"
	Name              = "dn"
	Tracker           = "tr"
)

type InvalidMagnetError string

func (error InvalidMagnetError) Error() string {
	return fmt.Sprintf("Invalid magnet link: %v", string(error))
}

type MagnetData struct {
	name           string
	trackers       []string
	hashes         map[int]string
	multiple_files bool
}

func toHash(hash []byte) string {
	return string(hash)
}
func ParseMagnetLink(link string) (MagnetData, error) {
	if link[:8] != "magnet:?" {
		return MagnetData{}, InvalidMagnetError("magnet links must start from `magnet:?`. Provide a valid magnet link")
	}

	link, err := url.QueryUnescape(link)
	if err != nil {
		return MagnetData{}, InvalidMagnetError("Could not unescape magnet link, check all symbol to be valid")
	}
	link = link[8:]
	result := MagnetData{
		trackers: []string{},
		hashes:   map[int]string{},
	}

	for {
		next_ampersand := strings.Index(link, "&")
		if len(link) < 4 { // min pair is xx=d
			return MagnetData{}, InvalidMagnetError(fmt.Sprintf("invalid remainding bytes: %v", link))
		}
		if link[:3] == "xt." {
			if len(link) < 6 { // min pair is xx.k=d
				return MagnetData{}, InvalidMagnetError(fmt.Sprintf("invalid remainding bytes: %v", link))
			} else if link[4] != '=' {
				return MagnetData{}, InvalidMagnetError(fmt.Sprintf("expected `=`, got %v instead. Left bytes: %v", string(link[4]), link[3:]))
			} else if len(result.hashes) != 0 || !result.multiple_files {
				return MagnetData{}, InvalidMagnetError("cannot mix `xt.NUM` and `xt` parameters.")
			}
			result.multiple_files = true
			hash_index := link[3]
			if hash_index >= '0' && hash_index <= '9' {
				hash_index = hash_index - '0'
			} else {
				return MagnetData{}, InvalidMagnetError(fmt.Sprintf("invalid hash index in xt: %v. Must be between 0 and 9.", hash_index))
			}
			var hash_end int
			if next_ampersand == -1 {
				hash_end = len(link)
			} else {
				hash_end = next_ampersand
			}

			hash := string(link[5:hash_end])
			if !strings.HasPrefix(hash, "urn:btih:") {
				return MagnetData{}, InvalidMagnetError(fmt.Sprintf("Currently, only support bittorent 1.0 hashes. Take a look at: https://en.wikipedia.org/wiki/Magnet_URI_scheme#:~:text=BitTorrent%20info%20hash%20%28BTIH"))
			}
			hash = string(hash[9:])

			if prev, ok := result.hashes[int(hash_index)]; ok {
				return MagnetData{}, InvalidMagnetError(fmt.Sprintf("the same hash index in xt is repeated: %v for hash (%v) and (%v)", prev, hash))
			}
			result.hashes[int(hash_index)] = hash
		} else if link[2] != '=' {
			return MagnetData{}, InvalidMagnetError(fmt.Sprintf("invalid remainding bytes: %v", link))
		} else {
			var value_end int
			if next_ampersand == -1 {
				value_end = len(link)
			} else {
				value_end = next_ampersand
			}
			value := link[3:value_end]

			switch MagnetKey(string(link[:2])) {
			case Hash:
				if result.multiple_files {
					return MagnetData{}, InvalidMagnetError("cannot mix `xt.NUM` and `xt` parameters.")
				}

				if _, ok := result.hashes[0]; ok {
					return MagnetData{}, InvalidMagnetError("cannot have two `xt` params in the link.")
				}

				if !strings.HasPrefix(value, "urn:btih:") {
					return MagnetData{}, InvalidMagnetError(fmt.Sprintf("Currently, only support bittorent 1.0 hashes. Take a look at: https://en.wikipedia.org/wiki/Magnet_URI_scheme#:~:text=BitTorrent%20info%20hash%20%28BTIH"))
				}
				hash := string(value[9:])
				result.hashes[0] = hash
			case Name:
				result.name = value
			case Tracker:
				result.trackers = append(result.trackers, value)
			default:
				// Ignore unknown parameters
			}
		}

		if next_ampersand == -1 {
			break
		}
		link = link[next_ampersand+1:]
	}
	return result, nil
}
