package openaicompat

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}

	text := ExtractOutputTextFromResponses(resp)

	usage := &dto.Usage{}
	if resp.Usage != nil {
		if resp.Usage.InputTokens != 0 {
			usage.PromptTokens = resp.Usage.InputTokens
			usage.InputTokens = resp.Usage.InputTokens
		}
		if resp.Usage.OutputTokens != 0 {
			usage.CompletionTokens = resp.Usage.OutputTokens
			usage.OutputTokens = resp.Usage.OutputTokens
		}
		if resp.Usage.TotalTokens != 0 {
			usage.TotalTokens = resp.Usage.TotalTokens
		} else {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
		if resp.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = resp.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.ImageTokens = resp.Usage.InputTokensDetails.ImageTokens
			usage.PromptTokensDetails.AudioTokens = resp.Usage.InputTokensDetails.AudioTokens
		}
		if resp.Usage.CompletionTokenDetails.ReasoningTokens != 0 {
			usage.CompletionTokenDetails.ReasoningTokens = resp.Usage.CompletionTokenDetails.ReasoningTokens
		}
	}

	created := resp.CreatedAt

	var toolCalls []dto.ToolCallResponse
	if text == "" && len(resp.Output) > 0 {
		for _, out := range resp.Output {
			if out.Type != "function_call" {
				continue
			}
			name := strings.TrimSpace(out.Name)
			if name == "" {
				continue
			}
			callId := strings.TrimSpace(out.CallId)
			if callId == "" {
				callId = strings.TrimSpace(out.ID)
			}
			toolCalls = append(toolCalls, dto.ToolCallResponse{
				ID:   callId,
				Type: "function",
				Function: dto.FunctionResponse{
					Name:      name,
					Arguments: out.ArgumentsString(),
				},
			})
		}
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	msg := dto.Message{
		Role:    "assistant",
		Content: BuildChatCompletionsMessageContentFromResponsesOutputs(resp.Output, text),
	}
	if len(toolCalls) > 0 {
		msg.SetToolCalls(toolCalls)
		msg.Content = ""
	}

	out := &dto.OpenAITextResponse{
		Id:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   resp.Model,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
		Usage: *usage,
	}

	return out, usage, nil
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder

	// Prefer assistant message outputs.
	for _, out := range resp.Output {
		if out.Type != "message" {
			continue
		}
		if out.Role != "" && out.Role != "assistant" {
			continue
		}
		for _, c := range out.Content {
			if c.Type == "output_text" && c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}
	for _, out := range resp.Output {
		for _, c := range out.Content {
			if c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	return sb.String()
}

func BuildChatCompletionsMessageContentFromResponses(resp *dto.OpenAIResponsesResponse) any {
	if resp == nil {
		return ""
	}
	return BuildChatCompletionsMessageContentFromResponsesOutputs(resp.Output, ExtractOutputTextFromResponses(resp))
}

func BuildChatCompletionsMessageContentFromResponsesOutputs(outputs []dto.ResponsesOutput, text string) any {
	images := extractResponseImages(outputs)
	if len(images) == 0 {
		return text
	}

	return buildResponseImagesMarkdown(text, images)
}

func buildResponseImagesMarkdown(text string, images []responseImage) string {
	parts := make([]string, 0, len(images))
	for _, image := range images {
		parts = append(parts, fmt.Sprintf("![generated image](%s)", image.URL))
	}
	if len(parts) == 0 {
		return text
	}
	return strings.Join(parts, "\n\n")
}

type responseImage struct {
	URL           string
	RevisedPrompt string
}

func extractResponseImages(outputs []dto.ResponsesOutput) []responseImage {
	images := make([]responseImage, 0)
	seen := make(map[string]struct{})
	for _, output := range outputs {
		if output.Type != dto.ResponsesOutputTypeImageGenerationCall {
			continue
		}
		images = append(images, extractResponseImagesFromOutput(output, seen)...)
	}
	return images
}

func extractResponseImagesFromOutput(output dto.ResponsesOutput, seen map[string]struct{}) []responseImage {
	images := make([]responseImage, 0)
	basePrompt := firstNonEmptyString(strings.TrimSpace(output.RevisedPrompt), extractOutputContentText(output.Content))
	baseMimeType := strings.TrimSpace(output.MimeType)

	appendImage := func(url string, revisedPrompt string) {
		url = strings.TrimSpace(url)
		if url == "" {
			return
		}
		if _, ok := seen[url]; ok {
			return
		}
		seen[url] = struct{}{}
		images = append(images, responseImage{
			URL:           url,
			RevisedPrompt: strings.TrimSpace(revisedPrompt),
		})
	}

	var walk func(value any, revisedPrompt string, mimeType string)
	walk = func(value any, revisedPrompt string, mimeType string) {
		switch v := value.(type) {
		case nil:
			return
		case string:
			if url := normalizeResponseImageURL(v, mimeType); url != "" {
				appendImage(url, revisedPrompt)
			}
		case []any:
			for _, item := range v {
				walk(item, revisedPrompt, mimeType)
			}
		case map[string]any:
			nestedPrompt := firstNonEmptyString(
				stringValue(v["revised_prompt"]),
				stringValue(v["prompt"]),
				revisedPrompt,
			)
			nestedMimeType := firstNonEmptyString(
				stringValue(v["mime_type"]),
				stringValue(v["mime"]),
				mimeType,
			)
			for _, key := range []string{"image_url", "url", "b64_json", "result", "data", "image", "images", "output", "items"} {
				if child, ok := v[key]; ok {
					walk(child, nestedPrompt, nestedMimeType)
				}
			}
		case json.RawMessage:
			walkJSONRawMessage(v, revisedPrompt, mimeType, walk)
		}
	}

	walk(output.ImageURL, basePrompt, baseMimeType)
	walk(output.Url, basePrompt, baseMimeType)
	walk(output.B64Json, basePrompt, baseMimeType)
	walk(output.Result, basePrompt, baseMimeType)
	walk(output.Data, basePrompt, baseMimeType)

	return images
}

func walkJSONRawMessage(raw json.RawMessage, revisedPrompt string, mimeType string, walk func(any, string, string)) {
	if len(raw) == 0 {
		return
	}
	var decoded any
	if err := common.Unmarshal(raw, &decoded); err == nil {
		walk(decoded, revisedPrompt, mimeType)
		return
	}
	walk(common.JsonRawMessageToString(raw), revisedPrompt, mimeType)
}

func extractOutputContentText(contents []dto.ResponsesOutputContent) string {
	if len(contents) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, content := range contents {
		if strings.TrimSpace(content.Text) == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(strings.TrimSpace(content.Text))
	}
	return sb.String()
}

func collectResponseImagePrompts(images []responseImage) string {
	if len(images) == 0 {
		return ""
	}
	prompts := make([]string, 0, len(images))
	seen := make(map[string]struct{})
	for _, image := range images {
		prompt := strings.TrimSpace(image.RevisedPrompt)
		if prompt == "" {
			continue
		}
		if _, ok := seen[prompt]; ok {
			continue
		}
		seen[prompt] = struct{}{}
		prompts = append(prompts, prompt)
	}
	return strings.Join(prompts, "\n\n")
}

func normalizeResponseImageURL(value string, mimeType string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(trimmed, "http://"), strings.HasPrefix(trimmed, "https://"):
		return trimmed
	case strings.HasPrefix(trimmed, "data:image/"):
		if url, err := storeGeneratedImageDataURL(trimmed); err == nil && url != "" {
			return url
		}
		return trimmed
	case looksLikeBase64ImagePayload(trimmed):
		normalizedMimeType := strings.TrimSpace(mimeType)
		if normalizedMimeType == "" {
			normalizedMimeType = "image/png"
		}
		if url, err := storeGeneratedImageBase64(trimmed, normalizedMimeType); err == nil && url != "" {
			return url
		}
		return fmt.Sprintf("data:%s;base64,%s", normalizedMimeType, trimmed)
	default:
		return ""
	}
}

func storeGeneratedImageDataURL(dataURL string) (string, error) {
	header, payload, ok := strings.Cut(dataURL, ",")
	if !ok {
		return "", fmt.Errorf("invalid image data url")
	}
	mimeType := strings.TrimPrefix(header, "data:")
	mimeType = strings.TrimSuffix(mimeType, ";base64")
	return storeGeneratedImageBase64(payload, mimeType)
}

func storeGeneratedImageBase64(payload string, mimeType string) (string, error) {
	imageBytes, err := decodeImageBase64(payload)
	if err != nil {
		return "", err
	}
	imageBytes, mimeType = optimizeGeneratedImageForServing(imageBytes, mimeType)

	dir := common.GetGeneratedImageDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	sum := sha256.Sum256(imageBytes)
	filename := hex.EncodeToString(sum[:]) + imageExtensionFromMimeType(mimeType)
	path := filepath.Join(dir, filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, imageBytes, 0644); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

	return generatedImagePublicURL(filename), nil
}

func decodeImageBase64(payload string) ([]byte, error) {
	cleaned := strings.TrimSpace(payload)
	cleaned = strings.TrimPrefix(cleaned, "base64,")
	cleaned = strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', '\t', ' ':
			return -1
		default:
			return r
		}
	}, cleaned)

	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, enc := range encodings {
		decoded, err := enc.DecodeString(cleaned)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func imageExtensionFromMimeType(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func optimizeGeneratedImageForServing(imageBytes []byte, mimeType string) ([]byte, string) {
	quality := generatedImageJPEGQuality()
	if quality <= 0 {
		return imageBytes, mimeType
	}
	normalizedMimeType := strings.ToLower(strings.TrimSpace(mimeType))
	switch normalizedMimeType {
	case "image/gif", "image/webp":
		return imageBytes, mimeType
	}

	img, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return imageBytes, mimeType
	}

	bounds := img.Bounds()
	canvas := image.NewRGBA(bounds)
	draw.Draw(canvas, bounds, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(canvas, bounds, img, bounds.Min, draw.Over)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: quality}); err != nil {
		return imageBytes, mimeType
	}
	optimized := buf.Bytes()
	if len(optimized) == 0 || len(optimized) >= len(imageBytes) {
		return imageBytes, mimeType
	}
	return optimized, "image/jpeg"
}

func generatedImageJPEGQuality() int {
	value := strings.TrimSpace(os.Getenv("GENERATED_IMAGE_JPEG_QUALITY"))
	if value == "" {
		return 85
	}
	quality, err := strconv.Atoi(value)
	if err != nil {
		return 85
	}
	if quality > 100 {
		return 100
	}
	return quality
}

func generatedImagePublicURL(filename string) string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("GENERATED_IMAGE_PUBLIC_BASE_URL")), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	}
	if base == "" || strings.Contains(base, "localhost") {
		return common.GeneratedImageRoutePrefix + filename
	}
	return base + common.GeneratedImageRoutePrefix + filename
}

func looksLikeBase64ImagePayload(value string) bool {
	if len(value) < 64 {
		return false
	}
	return !strings.ContainsAny(value, "{}[]:, \r\n\t")
}

func stringValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case json.RawMessage:
		return strings.TrimSpace(common.JsonRawMessageToString(v))
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
