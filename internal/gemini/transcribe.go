package gemini

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/jsonfault"
)

// Speech-to-text through generateContent.
//
// Google's OpenAI-compatibility layer does not serve /v1/audio/transcriptions,
// so a Gemini model that hears (gemini-3.5-transcribe, or any Gemini chat
// model with audio input) is reached through generateContent: the uploaded
// audio becomes an inlineData part, the client's prompt (or a fixed
// transcription instruction) a text part, and the text the model answers
// with is delivered in OpenAI's transcription shape. Only json and text are
// honoured as response formats: verbose_json, srt and vtt need timestamps a
// plain generateContent answer does not carry.

// TranscriptionFormatJSON and TranscriptionFormatText are the response
// formats a Gemini transcription can be delivered in.
const (
	TranscriptionFormatJSON = "json"
	TranscriptionFormatText = "text"
)

// ErrTranscriptionFormat reports a response_format the adapter cannot
// produce. The wrapped message names what it can, for the client.
var ErrTranscriptionFormat = errors.New("gemini: unsupported transcription response_format")

// ErrTranscriptionNoText reports a generateContent answer that carried no
// text: the provider answered, and its answer was not a transcript.
var ErrTranscriptionNoText = errors.New("gemini: no text in transcription response")

// defaultTranscriptionPrompt is the instruction sent to a chat model when
// the client gave no prompt of its own: asked nothing about an audio clip it
// tends to converse with it rather than transcribe it. A dedicated
// transcription model (gemini-3.5-transcribe) takes the audio alone and
// answers an instruction beside it with an empty reply, so it gets none.
const defaultTranscriptionPrompt = "Transcribe this audio verbatim. Reply with the transcript only."

// audioMimeByExt maps the upload's file extension to the mime type Gemini
// takes for that container. OpenAI clients commonly send the file as
// application/octet-stream, so the name is the reliable signal.
var audioMimeByExt = map[string]string{
	"wav": "audio/wav", "mp3": "audio/mp3", "mpeg": "audio/mpeg", "mpga": "audio/mpeg",
	"m4a": "audio/m4a", "aac": "audio/aac", "aiff": "audio/aiff", "aif": "audio/aiff",
	"ogg": "audio/ogg", "oga": "audio/ogg", "opus": "audio/opus", "flac": "audio/flac", "webm": "audio/webm",
}

// TranscriptionRequest is what the adapter needs off an OpenAI multipart
// transcription form.
type TranscriptionRequest struct {
	Audio          []byte
	FileName       string
	ContentType    string
	Prompt         string
	ResponseFormat string
	// Dedicated marks a transcription-only model, which is sent the audio
	// with no instruction beside it.
	Dedicated bool
}

// TranscriptionFormat resolves an OpenAI transcription response_format to
// one the adapter can deliver: absent means json, and anything but json or
// text is ErrTranscriptionFormat.
func TranscriptionFormat(format string) (string, error) {
	switch strings.ToLower(format) {
	case "", TranscriptionFormatJSON:
		return TranscriptionFormatJSON, nil
	case TranscriptionFormatText:
		return TranscriptionFormatText, nil
	}
	return "", fmt.Errorf("%w: %q; a Gemini transcription carries no timestamps, so response_format must be json or text", ErrTranscriptionFormat, format)
}

// AudioMimeType resolves the upload's mime type from its file name, falling
// back to the part's own content type when that names an audio container.
func AudioMimeType(fileName, contentType string) (string, bool) {
	if mime, ok := audioMimeByExt[strings.ToLower(strings.TrimPrefix(path.Ext(fileName), "."))]; ok {
		return mime, true
	}
	mime, _, _ := strings.Cut(contentType, ";")
	mime = strings.TrimSpace(strings.ToLower(mime))
	return mime, strings.HasPrefix(mime, "audio/")
}

// TranslateTranscriptionRequest maps an OpenAI transcription form onto a
// generateContent request, returning the format the answer is to be
// delivered in. An unsupported response_format is ErrTranscriptionFormat; an
// empty upload or one whose container cannot be named is a plain error, since
// the model would answer it with a 400 of its own.
func TranslateTranscriptionRequest(req TranscriptionRequest) (geminiBody []byte, format string, err error) {
	format, err = TranscriptionFormat(req.ResponseFormat)
	if err != nil {
		return nil, "", err
	}
	if len(req.Audio) == 0 {
		return nil, "", errors.New("gemini: transcription request carries no audio")
	}
	mime, ok := AudioMimeType(req.FileName, req.ContentType)
	if !ok {
		return nil, "", fmt.Errorf("gemini: cannot tell the audio container of %q; name the file with its extension (wav, mp3, m4a, ogg, flac, webm)", req.FileName)
	}
	parts := []genPart{{InlineData: &genBlob{MimeType: mime, Data: base64.StdEncoding.EncodeToString(req.Audio)}}}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" && !req.Dedicated {
		prompt = defaultTranscriptionPrompt
	}
	if prompt != "" {
		parts = append(parts, genPart{Text: prompt})
	}
	out := genRequest{Contents: []genContent{{Role: "user", Parts: parts}}}
	geminiBody, err = json.Marshal(out)
	if err != nil {
		return nil, "", fmt.Errorf("gemini: marshal transcription request: %w", err)
	}
	return geminiBody, format, nil
}

// BuildTranscriptionResponse turns a generateContent answer into the
// transcription body the client asked for, with its content type and the
// usage the answer carried. A chat model answers with text parts, a
// dedicated transcription model with audioTranscription parts; both are
// read. A blocked prompt or an answer without text is
// ErrTranscriptionNoText, wrapped with what the answer said instead; a body
// that is not a generateContent object at all is a plain decode error.
func BuildTranscriptionResponse(body []byte, format string) (out []byte, contentType string, usage SpeechUsage, err error) {
	var resp genResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, "", usage, fmt.Errorf("gemini: invalid transcription response: %s", jsonfault.Describe(err, len(body)))
	}
	if u := translateUsage(resp.UsageMetadata); u != nil {
		usage = SpeechUsage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens}
	}
	if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
		return nil, "", usage, fmt.Errorf("%w: prompt blocked (%s)", ErrTranscriptionNoText, resp.PromptFeedback.BlockReason)
	}
	var text strings.Builder
	for _, c := range resp.Candidates {
		for _, p := range c.Content.Parts {
			if p.AudioTranscription != nil {
				text.WriteString(p.AudioTranscription.Text)
				continue
			}
			if p.Thought || p.Text == "" {
				continue
			}
			text.WriteString(p.Text)
		}
	}
	transcript := strings.TrimSpace(text.String())
	if transcript == "" {
		detail := "no text part"
		if len(resp.Candidates) > 0 && resp.Candidates[0].FinishReason != "" {
			detail += " (finish reason " + resp.Candidates[0].FinishReason + ")"
		}
		return nil, "", usage, fmt.Errorf("%w: %s", ErrTranscriptionNoText, detail)
	}
	if format == TranscriptionFormatText {
		return []byte(transcript), "text/plain; charset=utf-8", usage, nil
	}
	out, err = json.Marshal(struct {
		Text string `json:"text"`
	}{transcript})
	if err != nil {
		return nil, "", usage, fmt.Errorf("gemini: marshal transcription: %w", err)
	}
	return out, "application/json", usage, nil
}
