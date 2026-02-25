package main

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

const (
	defaultSampleRateHz   = 16000
	defaultFrameDuration  = 20 * time.Millisecond
	defaultStartThreshold = 650.0
	defaultKeepThreshold  = 500.0
	defaultMinVoiceFrames = 2
	defaultHangoverFrames = 8
	defaultPreRollFrames  = 4
)

// VADConfig controls frame-level voice activity detection.
type VADConfig struct {
	SampleRateHz      int
	FrameDuration     time.Duration
	StartThreshold    float64
	ContinueThreshold float64
	MinVoiceFrames    int
	HangoverFrames    int
	PreRollFrames     int
}

func defaultVADConfig() VADConfig {
	return VADConfig{
		SampleRateHz:      defaultSampleRateHz,
		FrameDuration:     defaultFrameDuration,
		StartThreshold:    defaultStartThreshold,
		ContinueThreshold: defaultKeepThreshold,
		MinVoiceFrames:    defaultMinVoiceFrames,
		HangoverFrames:    defaultHangoverFrames,
		PreRollFrames:     defaultPreRollFrames,
	}
}

func (c VADConfig) Validate() error {
	if c.SampleRateHz <= 0 {
		return fmt.Errorf("invalid sample rate: %d", c.SampleRateHz)
	}
	if c.FrameDuration <= 0 {
		return fmt.Errorf("invalid frame duration: %s", c.FrameDuration)
	}
	frameMS := c.FrameDuration / time.Millisecond
	if frameMS != 10 && frameMS != 20 && frameMS != 30 {
		return fmt.Errorf("unsupported frame duration: %dms", frameMS)
	}
	if c.StartThreshold <= 0 || c.ContinueThreshold <= 0 {
		return fmt.Errorf("thresholds must be positive")
	}
	if c.ContinueThreshold > c.StartThreshold {
		return fmt.Errorf("continue threshold cannot exceed start threshold")
	}
	if c.MinVoiceFrames <= 0 {
		return fmt.Errorf("min voice frames must be >= 1")
	}
	if c.HangoverFrames < 0 {
		return fmt.Errorf("hangover frames must be >= 0")
	}
	if c.PreRollFrames < 0 {
		return fmt.Errorf("pre-roll frames must be >= 0")
	}
	return nil
}

func loadVADConfigFromEnv(logger *slog.Logger) VADConfig {
	cfg := defaultVADConfig()

	if v, ok := lookupIntEnv("VAD_SAMPLE_RATE_HZ"); ok {
		cfg.SampleRateHz = v
	}
	if v, ok := lookupIntEnv("VAD_FRAME_MS"); ok {
		cfg.FrameDuration = time.Duration(v) * time.Millisecond
	}
	if v, ok := lookupFloatEnv("VAD_START_THRESHOLD"); ok {
		cfg.StartThreshold = v
	}
	if v, ok := lookupFloatEnv("VAD_CONTINUE_THRESHOLD"); ok {
		cfg.ContinueThreshold = v
	}
	if v, ok := lookupIntEnv("VAD_MIN_VOICE_FRAMES"); ok {
		cfg.MinVoiceFrames = v
	}
	if v, ok := lookupIntEnv("VAD_HANGOVER_FRAMES"); ok {
		cfg.HangoverFrames = v
	}
	if v, ok := lookupIntEnv("VAD_PREROLL_FRAMES"); ok {
		cfg.PreRollFrames = v
	}

	if err := cfg.Validate(); err != nil {
		if logger != nil {
			logger.Warn("invalid VAD config from env; fallback to defaults", "error", err)
		}
		return defaultVADConfig()
	}

	if logger != nil {
		logger.Info(
			"VAD configured",
			"sampleRateHz", cfg.SampleRateHz,
			"frameMs", int(cfg.FrameDuration/time.Millisecond),
			"startThreshold", cfg.StartThreshold,
			"continueThreshold", cfg.ContinueThreshold,
			"minVoiceFrames", cfg.MinVoiceFrames,
			"hangoverFrames", cfg.HangoverFrames,
			"preRollFrames", cfg.PreRollFrames,
		)
	}

	return cfg
}

func lookupIntEnv(key string) (int, bool) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return v, true
}

func lookupFloatEnv(key string) (float64, bool) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// EnergyVAD uses average absolute amplitude for speech/non-speech decisions.
type EnergyVAD struct {
	cfg        VADConfig
	frameBytes int

	partial          []byte
	preRoll          [][]byte
	inSpeech         bool
	consecutiveVoice int
	hangoverRemain   int
}

func newEnergyVAD(cfg VADConfig) (*EnergyVAD, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	frameSamples := int((time.Duration(cfg.SampleRateHz) * cfg.FrameDuration) / time.Second)
	if frameSamples <= 0 {
		return nil, fmt.Errorf("invalid frame sample count")
	}

	return &EnergyVAD{
		cfg:        cfg,
		frameBytes: frameSamples * 2, // LINEAR16 mono
		partial:    make([]byte, 0, frameSamples*2),
	}, nil
}

func (v *EnergyVAD) Filter(chunk []byte) []byte {
	if len(chunk) == 0 {
		return nil
	}

	v.partial = append(v.partial, chunk...)

	out := make([]byte, 0, len(chunk))
	for len(v.partial) >= v.frameBytes {
		frame := v.partial[:v.frameBytes]
		emit := v.processFrame(frame)
		if len(emit) > 0 {
			out = append(out, emit...)
		}
		v.partial = v.partial[v.frameBytes:]
	}

	if len(v.partial) > 0 && len(v.partial) < v.frameBytes && cap(v.partial) > v.frameBytes*4 {
		tmp := make([]byte, len(v.partial))
		copy(tmp, v.partial)
		v.partial = tmp
	}

	return out
}

// Flush resets pending state. Partial frames are intentionally dropped.
func (v *EnergyVAD) Flush() {
	v.partial = nil
	v.preRoll = nil
	v.inSpeech = false
	v.consecutiveVoice = 0
	v.hangoverRemain = 0
}

func (v *EnergyVAD) processFrame(frame []byte) []byte {
	energy := meanAbsPCM16(frame)
	threshold := v.cfg.StartThreshold
	if v.inSpeech {
		threshold = v.cfg.ContinueThreshold
	}
	voiced := energy >= threshold

	if v.inSpeech {
		if voiced {
			v.hangoverRemain = v.cfg.HangoverFrames
			return cloneBytes(frame)
		}
		if v.hangoverRemain > 0 {
			v.hangoverRemain--
			out := cloneBytes(frame)
			if v.hangoverRemain == 0 {
				v.inSpeech = false
				v.consecutiveVoice = 0
				v.preRoll = nil
			}
			return out
		}

		v.inSpeech = false
		v.consecutiveVoice = 0
		v.preRoll = nil
		return nil
	}

	v.pushPreRoll(frame)

	if voiced {
		v.consecutiveVoice++
		if v.consecutiveVoice >= v.cfg.MinVoiceFrames {
			v.inSpeech = true
			v.hangoverRemain = v.cfg.HangoverFrames
			out := flattenFrames(v.preRoll)
			if len(out) == 0 {
				out = cloneBytes(frame)
			}
			v.preRoll = v.preRoll[:0]
			return out
		}
		return nil
	}

	v.consecutiveVoice = 0
	return nil
}

func (v *EnergyVAD) pushPreRoll(frame []byte) {
	if v.cfg.PreRollFrames <= 0 {
		return
	}

	copied := cloneBytes(frame)
	if len(v.preRoll) < v.cfg.PreRollFrames {
		v.preRoll = append(v.preRoll, copied)
		return
	}

	copy(v.preRoll, v.preRoll[1:])
	v.preRoll[len(v.preRoll)-1] = copied
}

func meanAbsPCM16(frame []byte) float64 {
	sampleCount := len(frame) / 2
	if sampleCount == 0 {
		return 0
	}

	var sum float64
	for i := 0; i < len(frame); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(frame[i : i+2]))
		if sample < 0 {
			sum += float64(-sample)
		} else {
			sum += float64(sample)
		}
	}
	return sum / float64(sampleCount)
}

func flattenFrames(frames [][]byte) []byte {
	totalBytes := 0
	for _, frame := range frames {
		totalBytes += len(frame)
	}
	if totalBytes == 0 {
		return nil
	}

	out := make([]byte, 0, totalBytes)
	for _, frame := range frames {
		out = append(out, frame...)
	}
	return out
}

func cloneBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}
