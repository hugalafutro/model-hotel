package gemini

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
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

// transcriptionInstruction is the text sent to a chat model beside the
// audio: asked nothing about a clip it converses with it rather than
// transcribing it, and OpenAI's prompt field is a context hint (names,
// spellings) rather than an instruction, so the client's prompt rides along
// as context under the instruction instead of replacing it (verified live:
// gemini-3.5-flash-lite answered a bare hint with "Understood. Your pass
// phrase is..."). A dedicated transcription model (gemini-3.5-transcribe)
// takes the audio alone and answers any text beside it with an empty reply,
// so it gets none.
const transcriptionInstruction = "Transcribe this audio verbatim. Reply with the transcript only."

// audioMimeByExt maps the upload's file extension to the mime type Gemini
// takes for that container. OpenAI clients commonly send the file as
// application/octet-stream, so the name is the reliable signal. mp4 is the
// same container as m4a, under the name OpenAI's own API lists.
var audioMimeByExt = map[string]string{
	"wav": "audio/wav", "mp3": "audio/mp3", "mpeg": "audio/mpeg", "mpga": "audio/mpeg",
	"m4a": "audio/m4a", "mp4": "audio/m4a", "aac": "audio/aac", "aiff": "audio/aiff", "aif": "audio/aiff",
	"ogg": "audio/ogg", "oga": "audio/ogg", "opus": "audio/opus", "flac": "audio/flac", "webm": "audio/webm",
}

// audioMimeAliases folds the content types browsers and curl send for the
// same containers onto Gemini's names; anything else that is not already a
// Gemini name is refused rather than passed on to the provider's 400.
var audioMimeAliases = map[string]string{
	"audio/x-wav": "audio/wav", "audio/wave": "audio/wav", "audio/vnd.wave": "audio/wav",
	"audio/x-m4a": "audio/m4a", "audio/mp4": "audio/m4a", "audio/x-aiff": "audio/aiff",
	"audio/x-flac": "audio/flac", "audio/mpeg3": "audio/mp3", "audio/x-mpeg-3": "audio/mp3",
}

// TranscriptionRequest is what the adapter needs off an OpenAI multipart
// transcription form. language has no counterpart on generateContent (the
// model detects it) and is dropped; timestamp_granularities only matters to
// the formats the adapter refuses.
type TranscriptionRequest struct {
	Audio          []byte
	FileName       string
	ContentType    string
	Prompt         string
	ResponseFormat string
	// Temperature is the form's temperature field as sent, mapped onto the
	// generation config when it parses as a number.
	Temperature string
	// Stream is the form's stream field: the adapter delivers one body, so a
	// streaming request is refused rather than quietly downgraded.
	Stream bool
	// Dedicated marks a transcription-only model, which is sent the audio
	// with no text beside it.
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
// back to the part's own content type when that names a container Gemini
// takes.
func AudioMimeType(fileName, contentType string) (string, bool) {
	if mime, ok := audioMimeByExt[strings.ToLower(strings.TrimPrefix(path.Ext(fileName), "."))]; ok {
		return mime, true
	}
	mime, _, _ := strings.Cut(contentType, ";")
	mime = strings.TrimSpace(strings.ToLower(mime))
	if alias, ok := audioMimeAliases[mime]; ok {
		return alias, true
	}
	for _, known := range audioMimeByExt {
		if mime == known {
			return mime, true
		}
	}
	return mime, false
}

// ValidateTranscriptionRequest checks what the adapter can serve without
// touching the upload's bytes, returning the container and the delivery
// format. An unsupported response_format is ErrTranscriptionFormat; a
// streaming request, an empty upload or one whose container cannot be named
// is a plain error, since the model would answer it with a 400 of its own.
// The refusal path runs this once per candidate, so the base64 of the upload
// is left to the translation proper.
func ValidateTranscriptionRequest(req TranscriptionRequest) (mime, format string, err error) {
	format, err = TranscriptionFormat(req.ResponseFormat)
	if err != nil {
		return "", "", err
	}
	if req.Stream {
		return "", "", errors.New("gemini: a transcription through generateContent is delivered whole; stream is not available")
	}
	if len(req.Audio) == 0 {
		return "", "", errors.New("gemini: transcription request carries no audio")
	}
	mime, ok := AudioMimeType(req.FileName, req.ContentType)
	if !ok {
		return "", "", errors.New("gemini: cannot tell the audio container; name the file with its extension (wav, mp3, m4a, ogg, flac, webm)")
	}
	return mime, format, nil
}

// TranslateTranscriptionRequest maps an OpenAI transcription form onto a
// generateContent request, returning the format the answer is to be
// delivered in. Its refusals are ValidateTranscriptionRequest's; a
// temperature that is not a finite number in Gemini's 0 to 2 range is
// dropped rather than refused, the way the unparsable one is.
func TranslateTranscriptionRequest(req TranscriptionRequest) (geminiBody []byte, format string, err error) {
	mime, format, err := ValidateTranscriptionRequest(req)
	if err != nil {
		return nil, "", err
	}
	parts := []genPart{{InlineData: &genBlob{MimeType: mime, Data: base64.StdEncoding.EncodeToString(req.Audio)}}}
	if !req.Dedicated {
		text := transcriptionInstruction
		if hint := strings.TrimSpace(req.Prompt); hint != "" {
			text += " Context: " + hint
		}
		parts = append(parts, genPart{Text: text})
	}
	out := genRequest{Contents: []genContent{{Role: "user", Parts: parts}}}
	if temp, err := strconv.ParseFloat(strings.TrimSpace(req.Temperature), 64); err == nil && temp >= 0 && temp <= 2 {
		out.GenerationConfig = &genConfig{Temperature: &temp}
	}
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
	// The first candidate alone: the request asks for one, and were a
	// second ever present it would be the same audio transcribed again.
	var text strings.Builder
	if len(resp.Candidates) > 0 {
		for _, p := range resp.Candidates[0].Content.Parts {
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
