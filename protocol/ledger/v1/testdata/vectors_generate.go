//go:build ignore

package main

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

const fixtureDirectory = "protocol/ledger/v1/testdata"

var fixtureNames = []string{
	"genesis-payload.cbor",
	"genesis-event-body.cbor",
	"genesis-record-body.cbor",
	"genesis-ledger-record.cbor",
}

type vectorManifest struct {
	Files map[string]string `json:"files"`
}

func main() {
	manifestBytes, err := os.ReadFile(filepath.Join(fixtureDirectory, "genesis-v1.json"))
	if err != nil {
		log.Fatal(err)
	}
	var manifest vectorManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		log.Fatal(err)
	}
	if len(manifest.Files) != len(fixtureNames) {
		log.Fatalf("fixture entries = %d, want %d", len(manifest.Files), len(fixtureNames))
	}
	allowed := make(map[string]struct{}, len(fixtureNames))
	for _, name := range fixtureNames {
		allowed[name] = struct{}{}
	}
	for name := range manifest.Files {
		if _, ok := allowed[name]; !ok {
			log.Fatalf("unexpected fixture name %q", name)
		}
	}
	for _, name := range fixtureNames {
		encodedHex, ok := manifest.Files[name]
		if !ok {
			log.Fatalf("missing fixture name %q", name)
		}
		encoded, err := hex.DecodeString(encodedHex)
		if err != nil {
			log.Fatalf("decode %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(fixtureDirectory, name), encoded, 0o644); err != nil {
			log.Fatalf("write %s: %v", name, err)
		}
	}
}
