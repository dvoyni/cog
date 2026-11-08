package recording_test

import (
	"errors"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
)

func TestPreprocessingAnswersTheFilesTextWithNoDirectivesHandled(t *testing.T) {
	source := "#define SKIN\n#if SKIN\nfn skinned() {}\n#endif\n"
	instance := recording.NewInstance()

	text, err := instance.PreprocessShader(fstest.MapFS{"s.wgsl": {Data: []byte(source)}}, "s.wgsl", nil)
	mustSucceed(t, err)

	if text != source {
		t.Errorf("PreprocessShader answered %q, want the file's text %q", text, source)
	}
}

func TestPreprocessingAMissingFileAnswersItsReadErrorAndRecordsTheCall(t *testing.T) {
	defines := []gfx.ShaderDefine{{Name: "SKIN"}}
	instance := recording.NewInstance()

	text, err := instance.PreprocessShader(fstest.MapFS{}, "gone.wgsl", defines)
	defines[0].Name = "CHANGED"

	if !errors.Is(err, fs.ErrNotExist) || text != "" {
		t.Fatalf("PreprocessShader answered %q, %v, want no text and fs.ErrNotExist", text, err)
	}
	want := []recording.Call{{
		Kind:      recording.CallKindPreprocessShader,
		Arguments: recording.PreprocessShaderArguments{Path: "gone.wgsl", Defines: []gfx.ShaderDefine{{Name: "SKIN"}}},
		Error:     err,
	}}
	if calls := instance.Recording().Calls; !reflect.DeepEqual(calls, want) {
		t.Errorf("recorded %+v, want %+v", calls, want)
	}
}
