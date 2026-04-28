package openaicompat

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
)

func TestResponsesResponseToChatCompletionsResponse_ImageGenerationCallBuildsMarkdownImage(t *testing.T) {
	t.Setenv("GENERATED_IMAGE_DIR", t.TempDir())
	t.Setenv("GENERATED_IMAGE_PUBLIC_BASE_URL", "https://example.test")

	resp := &dto.OpenAIResponsesResponse{
		CreatedAt: 1745762400,
		Model:     "gpt-5.4",
		Output: []dto.ResponsesOutput{
			{
				Type:          dto.ResponsesOutputTypeImageGenerationCall,
				RevisedPrompt: "A cute baby sea otter",
				Result: json.RawMessage(`[
					{
						"b64_json": "` + strings.Repeat("A", 128) + `"
					}
				]`),
			},
		},
	}

	chatResp, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl-test")
	if err != nil {
		t.Fatalf("ResponsesResponseToChatCompletionsResponse returned error: %v", err)
	}

	if len(chatResp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chatResp.Choices))
	}

	content, ok := chatResp.Choices[0].Message.Content.(string)
	if !ok {
		t.Fatalf("expected string content, got %T", chatResp.Choices[0].Message.Content)
	}
	if strings.Contains(content, "A cute baby sea otter") {
		t.Fatalf("did not expect prompt text in image-only content, got %q", content)
	}
	if !strings.Contains(content, "![generated image](") {
		t.Fatalf("expected markdown image in content, got %q", content)
	}
	urlStart := strings.Index(content, "https://example.test/generated-images/")
	if urlStart == -1 {
		t.Fatalf("expected generated image URL, got %q", content)
	}
	urlEnd := strings.Index(content[urlStart:], ")")
	if urlEnd == -1 {
		t.Fatalf("expected markdown image URL terminator, got %q", content)
	}
	urlValue := content[urlStart : urlStart+urlEnd]
	if !strings.HasPrefix(urlValue, "https://example.test/generated-images/") {
		t.Fatalf("expected generated image URL, got %q", urlValue)
	}
	if !strings.HasSuffix(urlValue, ".png") {
		t.Fatalf("expected png image URL, got %q", urlValue)
	}
	files, err := os.ReadDir(os.Getenv("GENERATED_IMAGE_DIR"))
	if err != nil {
		t.Fatalf("failed to read generated image dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 generated image file, got %d", len(files))
	}
}

func TestChatCompletionsRequestToResponsesRequest_DefaultsReasoningSummaryForGPT5(t *testing.T) {
	t.Setenv("RESPONSES_REASONING_SUMMARY", "")

	req := &dto.GeneralOpenAIRequest{Model: "gpt-5.4"}
	respReq, err := ChatCompletionsRequestToResponsesRequest(req)
	if err != nil {
		t.Fatalf("ChatCompletionsRequestToResponsesRequest returned error: %v", err)
	}
	if respReq.Reasoning == nil {
		t.Fatal("expected reasoning summary request")
	}
	if respReq.Reasoning.Summary != "detailed" {
		t.Fatalf("expected detailed summary, got %q", respReq.Reasoning.Summary)
	}
}

func TestChatCompletionsRequestToResponsesRequest_CanDisableDefaultReasoningSummary(t *testing.T) {
	t.Setenv("RESPONSES_REASONING_SUMMARY", "off")

	req := &dto.GeneralOpenAIRequest{Model: "gpt-5.4"}
	respReq, err := ChatCompletionsRequestToResponsesRequest(req)
	if err != nil {
		t.Fatalf("ChatCompletionsRequestToResponsesRequest returned error: %v", err)
	}
	if respReq.Reasoning != nil {
		t.Fatalf("expected no reasoning summary request, got %#v", respReq.Reasoning)
	}
}
