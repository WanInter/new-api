// relay-capture-decrypt exports encrypted relay artifacts as JSONL without a
// running new-api instance, database connection, or network access.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	relayCapturePurpose      = "relay-capture"
	segmentArchivePurpose    = "relay-capture-segment"
	segmentArchiveFileSuffix = ".tar.gz.enc"
)

type capturePart struct {
	Compression string `json:"compression,omitempty"`
	Stored      bool   `json:"stored"`
}

type captureManifest struct {
	ID        string      `json:"id"`
	CreatedAt int64       `json:"created_at"`
	Protocol  string      `json:"protocol"`
	Stream    bool        `json:"stream"`
	Request   capturePart `json:"request"`
	Response  capturePart `json:"response"`
}

type artifact struct {
	directory       string
	manifest        captureManifest
	archive         bool
	requestBody     []byte
	responseBody    []byte
	requestPresent  bool
	responsePresent bool
}

type conversation struct {
	Messages []map[string]any `json:"messages"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("relay-capture-decrypt", flag.ContinueOnError)
	flags.SetOutput(stderr)
	captureDir := flags.String("capture-dir", "", "directory to scan recursively for capture artifacts")
	captureRoot := flags.String("capture-root", "", "deprecated alias for --capture-dir")
	output := flags.String("output", "", "JSONL output file")
	force := flags.Bool("force", false, "overwrite an existing output file")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage:")
		fmt.Fprintln(stderr, "  relay-capture-decrypt --capture-dir CAPTURE_DIR --output conversations.jsonl [--force]")
		fmt.Fprintln(stderr, "Set CRYPTO_SECRET in the environment before running this tool.")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *output == "" || (*captureDir == "" && *captureRoot == "") || (*captureDir != "" && *captureRoot != "") {
		flags.Usage()
		return 2
	}
	secret := os.Getenv("CRYPTO_SECRET")
	if secret == "" {
		fmt.Fprintln(stderr, "CRYPTO_SECRET is required")
		return 2
	}
	common.CryptoSecret = secret

	scanDirectory := *captureDir
	if scanDirectory == "" {
		scanDirectory = *captureRoot
	}
	artifacts, err := findArtifacts(scanDirectory)
	if err != nil {
		fmt.Fprintf(stderr, "read capture directory: %v\n", err)
		return 1
	}

	file, err := openOutput(*output, *force)
	if err != nil {
		fmt.Fprintf(stderr, "open output: %v\n", err)
		return 1
	}
	defer file.Close()
	exported, skipped := 0, 0
	for _, item := range artifacts {
		records, exportErr := item.conversations()
		if exportErr != nil {
			skipped++
			fmt.Fprintf(stderr, "skip %s: %v\n", item.manifest.ID, exportErr)
			continue
		}
		for _, record := range records {
			line, marshalErr := common.Marshal(record)
			if marshalErr != nil {
				fmt.Fprintf(stderr, "encode %s: %v\n", item.manifest.ID, marshalErr)
				return 1
			}
			if _, err := file.Write(append(line, '\n')); err != nil {
				fmt.Fprintf(stderr, "write output: %v\n", err)
				return 1
			}
			exported++
		}
	}
	if err := file.Sync(); err != nil {
		fmt.Fprintf(stderr, "sync output: %v\n", err)
		return 1
	}
	if exported == 0 {
		fmt.Fprintln(stderr, "no complete conversations were exported")
		return 1
	}
	fmt.Fprintf(stdout, "exported %d conversation(s); skipped %d capture(s)\n", exported, skipped)
	return 0
}

func findArtifacts(root string) ([]artifact, error) {
	items := make([]artifact, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch {
		case entry.Name() == "manifest.json":
			item, err := loadArtifact(filepath.Dir(path))
			if err != nil {
				return err
			}
			items = append(items, item)
		case strings.HasSuffix(entry.Name(), segmentArchiveFileSuffix):
			archiveItems, err := loadSegmentArchive(path)
			if err != nil {
				return err
			}
			items = append(items, archiveItems...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no capture artifacts found in %s", root)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].manifest.CreatedAt == items[j].manifest.CreatedAt {
			return items[i].manifest.ID < items[j].manifest.ID
		}
		return items[i].manifest.CreatedAt < items[j].manifest.CreatedAt
	})
	return items, nil
}

func loadArtifact(directory string) (artifact, error) {
	body, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return artifact{}, err
	}
	var manifest captureManifest
	if err := common.Unmarshal(body, &manifest); err != nil {
		return artifact{}, err
	}
	if manifest.ID == "" || manifest.Protocol == "" {
		return artifact{}, errors.New("manifest is missing id or protocol")
	}
	return artifact{directory: directory, manifest: manifest}, nil
}

func (item artifact) conversations() ([]conversation, error) {
	if !item.manifest.Request.Stored || !item.manifest.Response.Stored {
		return nil, errors.New("request or response body was not stored")
	}
	if item.archive {
		if !item.requestPresent || !item.responsePresent {
			return nil, errors.New("archive is missing a stored request or response body")
		}
		return normalize(item.manifest.Protocol, item.requestBody, item.responseBody)
	}
	request, err := item.decrypt("request")
	if err != nil {
		return nil, err
	}
	response, err := item.decrypt("response")
	if err != nil {
		return nil, err
	}
	return normalize(item.manifest.Protocol, request, response)
}

func (item artifact) decrypt(part string) ([]byte, error) {
	ciphertext, err := os.ReadFile(filepath.Join(item.directory, part+".enc"))
	if err != nil {
		return nil, err
	}
	plaintext, err := common.DecryptSecret(strings.TrimSpace(string(ciphertext)), relayCapturePurpose)
	if err != nil {
		return nil, err
	}
	compression := item.manifest.Request.Compression
	if part == "response" {
		compression = item.manifest.Response.Compression
	}
	return decompressCapturePart([]byte(plaintext), compression)
}

func decompressCapturePart(body []byte, compression string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(compression)) {
	case "", "none":
		return body, nil
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		decompressed, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return decompressed, nil
	default:
		return nil, fmt.Errorf("unsupported capture compression %q", compression)
	}
}

func loadSegmentArchive(filename string) ([]artifact, error) {
	ciphertext, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	compressed, err := common.DecryptSecret(string(ciphertext), segmentArchivePurpose)
	if err != nil {
		return nil, err
	}
	reader, err := gzip.NewReader(bytes.NewReader([]byte(compressed)))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	items := make(map[string]*artifact)
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		parts := strings.Split(header.Name, "/")
		if len(parts) != 2 || !validCaptureID(parts[0]) || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) {
			return nil, errors.New("invalid relay capture segment entry")
		}
		body, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, err
		}
		item := items[parts[0]]
		if item == nil {
			item = &artifact{archive: true}
			items[parts[0]] = item
		}
		switch parts[1] {
		case "manifest.json":
			if item.manifest.ID != "" {
				return nil, errors.New("duplicate relay capture segment manifest")
			}
			if err := common.Unmarshal(body, &item.manifest); err != nil {
				return nil, err
			}
			if item.manifest.ID != parts[0] || item.manifest.Protocol == "" {
				return nil, errors.New("invalid relay capture segment manifest")
			}
		case "request":
			if item.requestPresent {
				return nil, errors.New("duplicate relay capture segment request")
			}
			item.requestBody = body
			item.requestPresent = true
		case "response":
			if item.responsePresent {
				return nil, errors.New("duplicate relay capture segment response")
			}
			item.responseBody = body
			item.responsePresent = true
		default:
			return nil, errors.New("invalid relay capture segment part")
		}
	}

	result := make([]artifact, 0, len(items))
	for _, item := range items {
		if item.manifest.ID == "" {
			return nil, errors.New("relay capture segment is missing a manifest")
		}
		if item.manifest.Request.Stored != item.requestPresent || item.manifest.Response.Stored != item.responsePresent {
			return nil, errors.New("relay capture segment payload metadata mismatch")
		}
		result = append(result, *item)
	}
	return result, nil
}

func validCaptureID(id string) bool {
	return id != "" && !strings.Contains(id, "..") && !strings.ContainsAny(id, "/\\")
}

func normalize(protocol string, request []byte, response []byte) ([]conversation, error) {
	var req map[string]any
	if err := common.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("decode request JSON: %w", err)
	}
	if looksLikeSSE(response) {
		return normalizeSSE(protocol, req, response)
	}
	var res map[string]any
	if err := common.Unmarshal(response, &res); err != nil {
		return nil, fmt.Errorf("decode response JSON: %w", err)
	}
	return normalizeJSON(protocol, req, res)
}

func normalizeJSON(protocol string, req map[string]any, res map[string]any) ([]conversation, error) {
	switch protocol {
	case "openai.chat_completions":
		messages, err := messagesFrom(req["messages"])
		if err != nil {
			return nil, err
		}
		choices, ok := res["choices"].([]any)
		if !ok || len(choices) == 0 {
			return nil, errors.New("response has no choices")
		}
		records := make([]conversation, 0, len(choices))
		for _, choice := range choices {
			choiceMap, ok := choice.(map[string]any)
			if !ok {
				return nil, errors.New("response choice is not an object")
			}
			message, err := messageFrom(choiceMap["message"])
			if err != nil || message["role"] != "assistant" {
				return nil, errors.New("response choice has no assistant message")
			}
			records = append(records, conversation{Messages: append(copyMessages(messages), message)})
		}
		return records, nil
	case "anthropic.messages":
		messages := make([]map[string]any, 0)
		if system, ok := req["system"]; ok && system != nil {
			messages = append(messages, map[string]any{"role": "system", "content": system})
		}
		requestMessages, err := messagesFrom(req["messages"])
		if err != nil {
			return nil, err
		}
		content, ok := res["content"]
		if !ok {
			return nil, errors.New("response has no content")
		}
		messages = append(messages, requestMessages...)
		messages = append(messages, map[string]any{"role": "assistant", "content": content})
		return []conversation{{Messages: messages}}, nil
	case "openai.responses":
		messages, err := responseInputMessages(req["input"])
		if err != nil {
			return nil, err
		}
		output, ok := res["output"].([]any)
		if !ok {
			return nil, errors.New("response has no output")
		}
		for _, item := range output {
			message, messageErr := messageFrom(item)
			if messageErr == nil && message["role"] == "assistant" {
				messages = append(messages, message)
			}
		}
		if len(messages) == 0 || messages[len(messages)-1]["role"] != "assistant" {
			return nil, errors.New("response has no assistant message")
		}
		return []conversation{{Messages: messages}}, nil
	default:
		return nil, fmt.Errorf("unsupported protocol %q", protocol)
	}
}

func normalizeSSE(protocol string, request map[string]any, response []byte) ([]conversation, error) {
	payloads, err := ssePayloads(response)
	if err != nil {
		return nil, err
	}
	switch protocol {
	case "openai.chat_completions":
		messages, err := messagesFrom(request["messages"])
		if err != nil {
			return nil, err
		}
		textByChoice := make(map[int]string)
		for _, payload := range payloads {
			choicesValue, exists := payload["choices"]
			if !exists {
				continue
			}
			choices, ok := choicesValue.([]any)
			if !ok {
				return nil, errors.New("stream choices are not an array")
			}
			for position, choiceValue := range choices {
				choice, ok := choiceValue.(map[string]any)
				if !ok {
					return nil, errors.New("stream choice is not an object")
				}
				index := streamIndex(choice["index"], position)
				delta, ok := choice["delta"].(map[string]any)
				if !ok {
					continue
				}
				if text, ok := delta["content"].(string); ok {
					textByChoice[index] += text
				}
			}
		}
		if len(textByChoice) == 0 {
			return nil, errors.New("stream has no assistant text")
		}
		indices := make([]int, 0, len(textByChoice))
		for index := range textByChoice {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		records := make([]conversation, 0, len(indices))
		for _, index := range indices {
			if textByChoice[index] == "" {
				continue
			}
			records = append(records, conversation{Messages: append(copyMessages(messages), map[string]any{
				"role":    "assistant",
				"content": textByChoice[index],
			})})
		}
		if len(records) == 0 {
			return nil, errors.New("stream has no assistant text")
		}
		return records, nil
	case "anthropic.messages":
		messages := make([]map[string]any, 0)
		if system, ok := request["system"]; ok && system != nil {
			messages = append(messages, map[string]any{"role": "system", "content": system})
		}
		requestMessages, err := messagesFrom(request["messages"])
		if err != nil {
			return nil, err
		}
		messages = append(messages, requestMessages...)
		var text strings.Builder
		for _, payload := range payloads {
			switch payload["type"] {
			case "content_block_start":
				if block, ok := payload["content_block"].(map[string]any); ok && block["type"] == "text" {
					if value, ok := block["text"].(string); ok {
						text.WriteString(value)
					}
				}
			case "content_block_delta":
				if delta, ok := payload["delta"].(map[string]any); ok {
					if value, ok := delta["text"].(string); ok {
						text.WriteString(value)
					}
				}
			}
		}
		if text.Len() == 0 {
			return nil, errors.New("stream has no assistant text")
		}
		messages = append(messages, map[string]any{"role": "assistant", "content": text.String()})
		return []conversation{{Messages: messages}}, nil
	case "openai.responses":
		messages, err := responseInputMessages(request["input"])
		if err != nil {
			return nil, err
		}
		textByOutput := make(map[int]string)
		for _, payload := range payloads {
			index := streamIndex(payload["output_index"], 0)
			switch payload["type"] {
			case "response.output_text.delta":
				if delta, ok := payload["delta"].(string); ok {
					textByOutput[index] += delta
				}
			case "response.output_text.done":
				if textByOutput[index] == "" {
					if text, ok := payload["text"].(string); ok {
						textByOutput[index] = text
					}
				}
			case "response.output_item.done":
				if textByOutput[index] == "" {
					if text, ok := outputText(payload["item"]); ok {
						textByOutput[index] = text
					}
				}
			case "response.completed":
				if completed, ok := payload["response"].(map[string]any); ok {
					appendResponseOutputText(textByOutput, completed["output"])
				}
			}
		}
		if len(textByOutput) == 0 {
			return nil, errors.New("stream has no assistant text")
		}
		indices := make([]int, 0, len(textByOutput))
		for index := range textByOutput {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		for _, index := range indices {
			if textByOutput[index] != "" {
				messages = append(messages, map[string]any{"role": "assistant", "content": textByOutput[index]})
			}
		}
		if len(messages) == 0 || messages[len(messages)-1]["role"] != "assistant" {
			return nil, errors.New("stream has no assistant text")
		}
		return []conversation{{Messages: messages}}, nil
	default:
		return nil, fmt.Errorf("unsupported protocol %q", protocol)
	}
}

type sseRecord struct {
	data string
}

func looksLikeSSE(body []byte) bool {
	text := strings.TrimLeft(string(body), " \t\r\n")
	return strings.HasPrefix(text, "data:") || strings.HasPrefix(text, "event:") || strings.HasPrefix(text, ":")
}

func ssePayloads(body []byte) ([]map[string]any, error) {
	events, err := parseSSE(body)
	if err != nil {
		return nil, err
	}
	payloads := make([]map[string]any, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.data) == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := common.Unmarshal([]byte(event.data), &payload); err != nil {
			return nil, fmt.Errorf("decode stream event JSON: %w", err)
		}
		payloads = append(payloads, payload)
	}
	if len(payloads) == 0 {
		return nil, errors.New("stream has no JSON events")
	}
	return payloads, nil
}

func parseSSE(body []byte) ([]sseRecord, error) {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	records := make([]sseRecord, 0)
	data := make([]string, 0)
	emit := func() {
		if len(data) > 0 {
			records = append(records, sseRecord{data: strings.Join(data, "\n")})
		}
		data = data[:0]
	}
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			emit()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, hasSeparator := strings.Cut(line, ":")
		if !hasSeparator {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		if field == "data" {
			data = append(data, value)
		}
	}
	emit()
	if len(records) == 0 {
		return nil, errors.New("stream has no data events")
	}
	return records, nil
}

func streamIndex(value any, fallback int) int {
	switch index := value.(type) {
	case float64:
		return int(index)
	case int:
		return index
	case int64:
		return int(index)
	default:
		return fallback
	}
}

func outputText(value any) (string, bool) {
	item, ok := value.(map[string]any)
	if !ok || item["type"] != "message" || item["role"] != "assistant" {
		return "", false
	}
	content, ok := item["content"].([]any)
	if !ok {
		return "", false
	}
	var text strings.Builder
	for _, value := range content {
		block, ok := value.(map[string]any)
		if !ok || (block["type"] != "output_text" && block["type"] != "text") {
			continue
		}
		if value, ok := block["text"].(string); ok {
			text.WriteString(value)
		}
	}
	return text.String(), text.Len() > 0
}

func appendResponseOutputText(textByOutput map[int]string, value any) {
	output, ok := value.([]any)
	if !ok {
		return
	}
	for index, item := range output {
		if textByOutput[index] != "" {
			continue
		}
		if text, ok := outputText(item); ok {
			textByOutput[index] = text
		}
	}
}

func responseInputMessages(input any) ([]map[string]any, error) {
	if text, ok := input.(string); ok {
		return []map[string]any{{"role": "user", "content": text}}, nil
	}
	if message, err := messageFrom(input); err == nil {
		return []map[string]any{message}, nil
	}
	return messagesFrom(input)
}

func messagesFrom(value any) ([]map[string]any, error) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil, errors.New("messages must be a non-empty array")
	}
	messages := make([]map[string]any, 0, len(items))
	for _, item := range items {
		message, err := messageFrom(item)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func messageFrom(value any) (map[string]any, error) {
	message, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("message is not an object")
	}
	role, ok := message["role"].(string)
	if !ok || role == "" {
		return nil, errors.New("message has no role")
	}
	content, ok := message["content"]
	if !ok {
		return nil, errors.New("message has no content")
	}
	result := map[string]any{"role": role, "content": content}
	for _, key := range []string{"name", "tool_call_id", "tool_calls"} {
		if value, ok := message[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func copyMessages(messages []map[string]any) []map[string]any {
	cloned := make([]map[string]any, len(messages))
	copy(cloned, messages)
	return cloned
}

func openOutput(path string, force bool) (*os.File, error) {
	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
