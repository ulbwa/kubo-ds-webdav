package webdavds

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// davEntry is one resource returned by a PROPFIND.
type davEntry struct {
	// Rel is the key-relative path (no leading slash), e.g. "blocks/za/CIQ...".
	Rel   string
	IsDir bool
	Size  int64
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?>` +
	`<d:propfind xmlns:d="DAV:"><d:prop>` +
	`<d:resourcetype/><d:getcontentlength/>` +
	`</d:prop></d:propfind>`

type msMultistatus struct {
	XMLName   xml.Name     `xml:"DAV: multistatus"`
	Responses []msResponse `xml:"DAV: response"`
}

type msResponse struct {
	Href     string       `xml:"DAV: href"`
	Propstat []msPropstat `xml:"DAV: propstat"`
}

type msPropstat struct {
	Status string `xml:"DAV: status"`
	Prop   msProp `xml:"DAV: prop"`
}

type msProp struct {
	Collection    *xml.Name `xml:"DAV: resourcetype>collection"`
	ContentLength string    `xml:"DAV: getcontentlength"`
}

// propfind issues a PROPFIND at depth (0, 1, or <0 for infinity) and returns
// the resources found, excluding the queried collection itself for depth>0.
func (c *client) propfind(ctx context.Context, p string, depth int) ([]davEntry, error) {
	d := "0"
	switch {
	case depth == 1:
		d = "1"
	case depth < 0:
		d = "infinity"
	}
	// Listing a collection requires a trailing slash, else servers (Apache
	// mod_dav) answer with a 301 redirect to the slash form. Compute the slash
	// on the full (root-prefixed) path so the root collection is covered too.
	full := c.fullPath(p)
	if depth != 0 && full != "" && !strings.HasSuffix(full, "/") {
		full += "/"
	}

	var ms msMultistatus
	err := c.withRetry(ctx, func() error {
		resp, err := c.doAbs(ctx, "PROPFIND", full, []byte(propfindBody), map[string]string{
			"Depth":        d,
			"Content-Type": "application/xml; charset=utf-8",
		})
		if err != nil {
			return err
		}
		defer drain(resp)
		if resp.StatusCode == http.StatusNotFound {
			return errNotFound
		}
		if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode/100 != 2 {
			return statusErr("PROPFIND", p, resp)
		}
		body, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			return rerr
		}
		ms = msMultistatus{}
		return xml.Unmarshal(body, &ms)
	})
	if err != nil {
		return nil, err
	}

	self := strings.Trim(c.fullPath(p), "/")
	out := make([]davEntry, 0, len(ms.Responses))
	for _, r := range ms.Responses {
		hrefPath := r.Href
		if u, perr := url.Parse(r.Href); perr == nil {
			hrefPath = u.Path
		}
		rel := c.relPath(hrefPath)
		e := davEntry{Rel: rel}
		for _, ps := range r.Propstat {
			if !strings.Contains(ps.Status, " 200 ") {
				continue
			}
			if ps.Prop.Collection != nil {
				e.IsDir = true
			}
			if n, perr := strconv.ParseInt(strings.TrimSpace(ps.Prop.ContentLength), 10, 64); perr == nil {
				e.Size = n
			}
		}
		// At depth>0 the server echoes the queried collection; skip it.
		if depth != 0 && self == c.relToFull(rel) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// relToFull converts a key-relative path back to a root-prefixed path for
// comparison with the queried collection.
func (c *client) relToFull(rel string) string {
	return strings.Trim(c.fullPath(rel), "/")
}
