package gemini

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/jsonfault"
)

// Text-to-speech through generateContent.
//
// Google's OpenAI-compatibility layer does not serve /v1/audio/speech, so a
// Gemini TTS model (gemini-2.5-flash-preview-tts and kin) is reached the way
// LiteLLM reaches it: the OpenAI speech request becomes a generateContent
// call asking for an AUDIO response with a speechConfig naming the voice, and
// the audio comes back as an inlineData part of raw 16-bit PCM
// (audio/L16;codec=pcm;rate=24000, mono). That is the only encoding the
// model produces, so the OpenAI response_format is honoured for wav (the PCM
// under a RIFF header) and pcm (the bytes as they are) and refused for the
// compressed formats, which would need an encoder this gateway does not
// carry. speed and instructions have no counterpart on the native call and
// are ignored; Gemini takes delivery style from the text itself.

// SpeechFormatWAV and SpeechFormatPCM are the response formats a Gemini TTS
// model can honour.
const (
	SpeechFormatWAV = "wav"
	SpeechFormatPCM = "pcm"
)

// ErrSpeechFormat reports a response_format the model cannot produce. The
// wrapped message names what it can, for the client.
var ErrSpeechFormat = errors.New("gemini: unsupported speech response_format")

// ErrSpeechNoAudio reports a generateContent answer that carried no audio
// part: the provider answered, and its answer was not speech.
var ErrSpeechNoAudio = errors.New("gemini: no audio in speech response")

// defaultSpeechVoice is the voice used when the request names none.
const defaultSpeechVoice = "Kore"

// openAIVoices maps the voices OpenAI's own TTS models take, which its SDKs
// and every client written against them send by default, onto Gemini's
// prebuilt voices. Any other name is passed through as a Gemini voice name,
// so a client that knows the Gemini voices can name one directly.
var openAIVoices = map[string]string{
	"alloy":   "Kore",
	"ash":     "Orus",
	"ballad":  "Enceladus",
	"coral":   "Autonoe",
	"echo":    "Puck",
	"fable":   "Aoede",
	"onyx":    "Charon",
	"nova":    "Leda",
	"sage":    "Callirrhoe",
	"shimmer": "Zephyr",
	"verse":   "Fenrir",
	"cedar":   "Iapetus",
	"marin":   "Despina",
}

type oaiSpeechRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format"`
}

type genSpeechRequest struct {
	Contents         []genContent    `json:"contents"`
	GenerationConfig genSpeechConfig `json:"generationConfig"`
}

type genSpeechConfig struct {
	ResponseModalities []string `json:"responseModalities"`
	SpeechConfig       struct {
		VoiceConfig struct {
			PrebuiltVoiceConfig struct {
				VoiceName string `json:"voiceName"`
			} `json:"prebuiltVoiceConfig"`
		} `json:"voiceConfig"`
	} `json:"speechConfig"`
}

// SpeechVoice resolves an OpenAI speech request's voice to a Gemini prebuilt
// voice name.
func SpeechVoice(voice string) string {
	if voice == "" {
		return defaultSpeechVoice
	}
	if mapped, ok := openAIVoices[strings.ToLower(voice)]; ok {
		return mapped
	}
	return voice
}

// SpeechFormat resolves an OpenAI speech request's response_format to one
// the model can honour: absent means wav (OpenAI's default is mp3, which
// needs an encoder; wav is what every player takes in its place), and
// anything but wav or pcm is ErrSpeechFormat.
func SpeechFormat(format string) (string, error) {
	switch strings.ToLower(format) {
	case "", SpeechFormatWAV:
		return SpeechFormatWAV, nil
	case SpeechFormatPCM:
		return SpeechFormatPCM, nil
	}
	return "", fmt.Errorf("%w: %q; a Gemini TTS model produces PCM audio, so response_format must be wav or pcm", ErrSpeechFormat, format)
}

// TranslateSpeechRequest maps an OpenAI /v1/audio/speech body onto a
// generateContent request for the model it names, returning the format the
// answer is to be delivered in. An unsupported response_format is
// ErrSpeechFormat; an empty input is a plain error, since the model would
// answer it with a 400 of its own.
func TranslateSpeechRequest(body []byte) (geminiBody []byte, model, format string, err error) {
	var req oaiSpeechRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, "", "", fmt.Errorf("gemini: invalid speech request: %s", jsonfault.Describe(err, len(body)))
	}
	if strings.TrimSpace(req.Input) == "" {
		return nil, "", "", errors.New("gemini: speech request carries no input")
	}
	format, err = SpeechFormat(req.ResponseFormat)
	if err != nil {
		return nil, "", "", err
	}
	out := genSpeechRequest{Contents: []genContent{{Role: "user", Parts: []genPart{{Text: req.Input}}}}}
	out.GenerationConfig.ResponseModalities = []string{"AUDIO"}
	out.GenerationConfig.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName = SpeechVoice(req.Voice)
	geminiBody, err = json.Marshal(out)
	if err != nil {
		return nil, "", "", fmt.Errorf("gemini: marshal speech request: %w", err)
	}
	return geminiBody, req.Model, format, nil
}

// SpeechUsage is the token usage a speech answer reported, for metering.
type SpeechUsage struct {
	PromptTokens     int
	CompletionTokens int
}

// BuildSpeechResponse turns a generateContent answer into the audio bytes
// the client asked for, with their content type and the usage the answer
// carried. A blocked prompt or an answer without an audio part is
// ErrSpeechNoAudio, wrapped with what the answer said instead; a body that
// is not a generateContent object at all is a plain decode error.
func BuildSpeechResponse(body []byte, format string) (audio []byte, contentType string, usage SpeechUsage, err error) {
	var resp genResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, "", usage, fmt.Errorf("gemini: invalid speech response: %s", jsonfault.Describe(err, len(body)))
	}
	if u := translateUsage(resp.UsageMetadata); u != nil {
		usage = SpeechUsage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens}
	}
	if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
		return nil, "", usage, fmt.Errorf("%w: prompt blocked (%s)", ErrSpeechNoAudio, resp.PromptFeedback.BlockReason)
	}
	for _, c := range resp.Candidates {
		for _, p := range c.Content.Parts {
			if p.InlineData == nil || p.InlineData.Data == "" || !strings.HasPrefix(p.InlineData.MimeType, "audio/") {
				continue
			}
			pcm, err := base64.StdEncoding.DecodeString(p.InlineData.Data)
			if err != nil {
				return nil, "", usage, fmt.Errorf("gemini: speech audio is not base64: %w", err)
			}
			if format == SpeechFormatPCM {
				return pcm, "audio/pcm", usage, nil
			}
			return wavFromPCM(pcm, sampleRateOf(p.InlineData.MimeType)), "audio/wav", usage, nil
		}
	}
	detail := "no audio part"
	if len(resp.Candidates) > 0 && resp.Candidates[0].FinishReason != "" {
		detail += " (finish reason " + resp.Candidates[0].FinishReason + ")"
	}
	return nil, "", usage, fmt.Errorf("%w: %s", ErrSpeechNoAudio, detail)
}

// defaultSampleRate is what Gemini TTS produces, and what a mime type that
// names no rate, or one outside the range any PCM stream uses, is taken to
// mean; the bounds also keep the WAV header's 32-bit fields honest.
const (
	defaultSampleRate = 24000
	minSampleRate     = 8000
	maxSampleRate     = 192000
)

// sampleRateOf reads the rate parameter off an L16 mime type
// (audio/L16;codec=pcm;rate=24000).
func sampleRateOf(mimeType string) int {
	for _, param := range strings.Split(mimeType, ";")[1:] {
		k, v, ok := strings.Cut(strings.TrimSpace(param), "=")
		if !ok || !strings.EqualFold(k, "rate") {
			continue
		}
		if rate, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && rate >= minSampleRate && rate <= maxSampleRate {
			return rate
		}
	}
	return defaultSampleRate
}

// wavFromPCM wraps 16-bit mono PCM in a RIFF/WAVE header.
func wavFromPCM(pcm []byte, sampleRate int) []byte {
	const channels, bitsPerSample = 1, 16
	blockAlign := channels * bitsPerSample / 8
	header := make([]byte, 44)
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+len(pcm))) //nolint:gosec // G115: a WAV data chunk cannot reach 4 GiB here
	copy(header[8:], "WAVE")
	copy(header[12:], "fmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1) // PCM
	binary.LittleEndian.PutUint16(header[22:], channels)
	binary.LittleEndian.PutUint32(header[24:], uint32(sampleRate))            //nolint:gosec // G115: a sample rate is small and positive
	binary.LittleEndian.PutUint32(header[28:], uint32(sampleRate*blockAlign)) //nolint:gosec // G115: as above
	binary.LittleEndian.PutUint16(header[32:], uint16(blockAlign))            //nolint:gosec // G115: 2
	binary.LittleEndian.PutUint16(header[34:], bitsPerSample)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(len(pcm))) //nolint:gosec // G115: as the RIFF size above
	return append(header, pcm...)
}
