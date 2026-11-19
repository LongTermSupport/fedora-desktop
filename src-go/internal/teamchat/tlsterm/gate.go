package tlsterm

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// The request gate (T10, Plan 00257 SEC-5): the terminator relays HTTP/1.1 requests whose target
// is the Matrix API a member, Element and the health probe use, and nothing else. Each request's
// line and headers are read and judged before a byte of it reaches the homeserver, and its body
// is found by its own framing, so a second request on the connection is judged as the first was.
// The bytes are relayed as the client sent them; the answers come back unread.

const (
	// RequestLimit is how long a client has, after its handshake, to send its first request's
	// line and headers; the upstream is dialled only once it has (T10).
	RequestLimit = 10 * time.Second
	// maxHead caps one request's line and headers together.
	maxHead = 64 << 10
	// maxLine caps one line of a head or of a chunked body's framing.
	maxLine = 16 << 10
)

// allowedTargets are the request targets the terminator relays: the Matrix client-server and
// media APIs (`/_matrix/`) and the well-known discovery (`/.well-known/matrix/`). The
// homeserver's admin APIs (`/_synapse/admin/`, `/_tuwunel/`) are reached on loopback only.
var allowedTargets = []string{"/_matrix/", "/.well-known/matrix/"}

// refusal is a request the gate does not relay, and why; status is the HTTP answer a first
// request gets.
type refusal struct {
	status int
	why    string
}

func (r *refusal) Error() string { return r.why }

func refuse(status int, format string, args ...any) error {
	return &refusal{status: status, why: fmt.Sprintf(format, args...)}
}

// head is one request's line and headers, as the client sent them, and how its body is framed.
type head struct {
	raw     []byte
	chunked bool
	length  int64
}

// readHead reads the next request's head from br: io.EOF when the client ended between
// requests, a *refusal for a request the gate does not relay, any other error a broken stream.
func readHead(br *bufio.Reader) (head, error) {
	var h head
	first := true
	var version string
	var lengths, encodings []string
	for {
		line, err := readLine(br)
		if err != nil {
			if first && errors.Is(err, io.EOF) && len(line) == 0 {
				return head{}, io.EOF
			}
			return head{}, err
		}
		h.raw = append(h.raw, line...)
		if len(h.raw) > maxHead {
			return head{}, refuse(431, "the request's head is over %d bytes", maxHead)
		}
		text := string(line[:len(line)-2])
		if first {
			first = false
			if version, err = judgeLine(text); err != nil {
				return head{}, err
			}
			continue
		}
		if text == "" {
			break
		}
		name, value, err := header(text)
		if err != nil {
			return head{}, err
		}
		switch strings.ToLower(name) {
		case "content-length":
			lengths = append(lengths, value)
		case "transfer-encoding":
			encodings = append(encodings, value)
		case "upgrade":
			return head{}, refuse(400, "a protocol upgrade is not relayed")
		}
	}
	return h, h.frame(version, lengths, encodings)
}

// frame is how the body is found (RFC 9112 section 6): chunked, a single length, or none; any
// framing two readers could read two ways is refused, so the homeserver never sees a request
// the gate did not judge. A transfer encoding in an HTTP/1.0 request is faulty framing (6.1).
func (h *head) frame(version string, lengths, encodings []string) error {
	switch {
	case len(encodings) > 0 && version == "HTTP/1.0":
		return refuse(400, "an HTTP/1.0 request with a transfer encoding is not relayed")
	case len(encodings) > 0 && len(lengths) > 0:
		return refuse(400, "a request with both a length and a transfer encoding is not relayed")
	case len(encodings) > 1 || len(encodings) == 1 && !strings.EqualFold(strings.TrimSpace(encodings[0]), "chunked"):
		return refuse(400, "a transfer encoding other than chunked is not relayed")
	case len(encodings) == 1:
		h.chunked = true
	case len(lengths) > 1:
		return refuse(400, "a request with more than one length is not relayed")
	case len(lengths) == 1:
		n, err := strconv.ParseInt(strings.TrimSpace(lengths[0]), 10, 64)
		if err != nil || n < 0 || strings.TrimSpace(lengths[0]) != strconv.FormatInt(n, 10) {
			return refuse(400, "the request's length is not a number")
		}
		h.length = n
	}
	return nil
}

// judgeLine holds the request line to `METHOD target HTTP/1.x` with a target the gate relays,
// and answers its version.
func judgeLine(line string) (string, error) {
	parts := strings.Split(line, " ")
	if len(parts) != 3 || parts[0] == "" || (parts[2] != "HTTP/1.1" && parts[2] != "HTTP/1.0") {
		return "", refuse(400, "the request line is not an HTTP/1.1 request")
	}
	if parts[0] == "CONNECT" {
		return "", refuse(405, "CONNECT is not relayed")
	}
	return parts[2], judgeTarget(parts[1])
}

// judgeTarget holds a request target to an origin form under one of allowedTargets, with no dot
// segment, spelt or escaped, by which a path that starts allowed could route elsewhere.
func judgeTarget(target string) error {
	path, _, _ := strings.Cut(target, "?")
	allowed := false
	for _, prefix := range allowedTargets {
		if strings.HasPrefix(path, prefix) {
			allowed = true
		}
	}
	if !allowed {
		return refuse(403, "the path %s is not the Matrix API this terminator serves", firstSegments(path))
	}
	for seg := range strings.SplitSeq(path, "/") {
		if seg = strings.ReplaceAll(strings.ToLower(seg), "%2e", "."); seg == "." || seg == ".." {
			return refuse(403, "a path with a dot segment is not relayed")
		}
	}
	return nil
}

// firstSegments is at most the first two segments of path, what a refusal names: a path may
// carry a user's id or a token's place, which the log never gets.
func firstSegments(path string) string {
	parts := strings.SplitN(path, "/", 4)
	if len(parts) > 3 {
		parts = parts[:3]
	}
	out := strings.Join(parts, "/")
	if len(out) > 64 {
		out = out[:64]
	}
	return strconv.Quote(out)
}

// header splits one header line; a folded line or a name with a space is refused.
func header(line string) (string, string, error) {
	name, value, found := strings.Cut(line, ":")
	if !found || name == "" || strings.ContainsAny(name, " \t") {
		return "", "", refuse(400, "a header line is not a name and a value")
	}
	return name, strings.TrimSpace(value), nil
}

// readLine is one CRLF-ended line, its ending kept, at most maxLine bytes.
func readLine(br *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := br.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > maxLine {
			return nil, refuse(431, "a line is over %d bytes", maxLine)
		}
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case err != nil:
			if len(line) > 0 && errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return line, err
		}
		if !bytes.HasSuffix(line, []byte("\r\n")) {
			return nil, refuse(400, "a line does not end in CRLF")
		}
		return line, nil
	}
}

// relayBody copies the body h frames from br to dst as it came.
func relayBody(dst io.Writer, br *bufio.Reader, h head) error {
	if !h.chunked {
		_, err := io.CopyN(dst, br, h.length)
		return err
	}
	for {
		line, err := readLine(br)
		if err != nil {
			return unexpected(err)
		}
		size, err := chunkSize(line)
		if err != nil {
			return err
		}
		if _, err := dst.Write(line); err != nil {
			return err
		}
		if size == 0 {
			return relayTrailers(dst, br)
		}
		if _, err := io.CopyN(dst, br, size); err != nil {
			return unexpected(err)
		}
		end, err := readLine(br)
		if err != nil {
			return unexpected(err)
		}
		if len(end) != 2 {
			return refuse(400, "a chunk does not end in CRLF")
		}
		if _, err := dst.Write(end); err != nil {
			return err
		}
	}
}

// chunkSize is the size a chunk's size line gives: hexadecimal digits alone before any
// extension (RFC 9112 section 7.1), at most 15 of them so it fits an int64. A sign, a space or a
// prefix is refused, so the gate and the homeserver cannot read one size two ways.
func chunkSize(line []byte) (int64, error) {
	text := string(line[:len(line)-2])
	if i := strings.IndexByte(text, ';'); i >= 0 {
		text = text[:i]
	}
	if text == "" || len(text) > 15 || strings.TrimLeft(text, "0123456789abcdefABCDEF") != "" {
		return 0, refuse(400, "a chunk's size is not hexadecimal")
	}
	return strconv.ParseInt(text, 16, 64)
}

// relayTrailers copies a chunked body's trailer lines through its empty last line.
func relayTrailers(dst io.Writer, br *bufio.Reader) error {
	for total := 0; ; {
		line, err := readLine(br)
		if err != nil {
			return unexpected(err)
		}
		if total += len(line); total > maxHead {
			return refuse(431, "the trailers are over %d bytes", maxHead)
		}
		if _, err := dst.Write(line); err != nil {
			return err
		}
		if len(line) == 2 {
			return nil
		}
	}
}

// unexpected is an end of the stream inside a request, which is a broken one.
func unexpected(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// answer is the HTTP answer to a refused first request, the connection closed after it.
func answer(r *refusal) []byte {
	code := "M_FORBIDDEN"
	if r.status != 403 {
		code = "M_UNRECOGNIZED"
	}
	body := `{"errcode":"` + code + `","error":` + strconv.Quote("teamchat terminator: "+r.why) + `}`
	return []byte("HTTP/1.1 " + strconv.Itoa(r.status) + " " + statusText(r.status) + "\r\n" +
		"Content-Type: application/json\r\nContent-Length: " + strconv.Itoa(len(body)) + "\r\nConnection: close\r\n\r\n" + body)
}

func statusText(status int) string {
	switch status {
	case 403:
		return "Forbidden"
	case 405:
		return "Method Not Allowed"
	case 431:
		return "Request Header Fields Too Large"
	}
	return "Bad Request"
}
