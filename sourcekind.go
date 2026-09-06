package kgraph

import (
	"path/filepath"
	"strings"
)

// How a source file may be presented to a reader.
//
// Lifted out of the retired kgweb viewer, which is where it was written. It is
// not viewer-specific — `kg verify`'s attestation workbench asks the same
// question of the same files — and leaving it in a package about publishing was
// what made it look like it was.

// textExt is what may be served verbatim as text.
//
// An .eml is the case that motivates it: a reader asking "what did this email
// actually say" wants the headers, the routing and the quoted chain, not a
// tidied body. Serving the file unparsed is the only thing that cannot silently
// drop part of it.
var textExt = map[string]bool{
	".txt": true, ".md": true, ".eml": true, ".html": true, ".htm": true,
	".csv": true, ".json": true, ".log": true, ".rtf": true,
}

var imageExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

func sourceKind(rel string) string {
	switch ext := strings.ToLower(filepath.Ext(rel)); {
	case ext == ".pdf":
		return "pdf"
	case textExt[ext]:
		return "text"
	case imageExt[ext]:
		return "image"
	default:
		return "binary"
	}
}
