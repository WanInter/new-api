// relay-capture-decrypt exports encrypted relay artifacts as JSONL without a
// running new-api instance, database connection, or network access.
package main

import (
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

const relayCapturePurpose = "relay-capture"

type capturePart struct {
	Stored bool `json:"stored"`
}

type captureManifest struct {
	ID        string      `json:"id"`
	CreatedAt int64       `json:"created_at"`
	Protocol  string      `json:"protocol"`
	Request   capturePart `json:"request"`
	Response  capturePart `json:"response"`
}

type artifact struct {
	directory string
	manifest  captureManifest
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
		if entry.IsDir() || entry.Name() != "manifest.json" {
			return nil
		}
		item, err := loadArtifact(filepath.Dir(path))
		if err != nil {
			return err
		}
		items = append(items, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no capture manifests found in %s", root)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].manifest.CreatedAt < items[j].manifest.CreatedAt })
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
	return []byte(plaintext), nil
}

func normalize(protocol string, request []byte, response []byte) ([]conversation, error) {
	var req map[string]any
	if err := common.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("decode request JSON: %w", err)
	}
	var res map[string]any
	if err := common.Unmarshal(response, &res); err != nil {
		return nil, fmt.Errorf("decode response JSON: %w", err)
	}
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
