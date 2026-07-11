package releaseworkflow

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDockerReleaseWritesExactTagOnlyAfterFinalImmutabilityCheck(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate workflow test")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "docker-build.yml")
	raw, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read docker workflow: %v", err)
	}
	workflow := string(raw)

	buildStart := mustIndex(t, workflow, "  build_docker_image:")
	smokeStart := mustIndex(t, workflow, "  release_smoke:")
	promoteStart := mustIndex(t, workflow, "  promote_channel:")
	if !(buildStart < smokeStart && smokeStart < promoteStart) {
		t.Fatal("docker release jobs are not in the expected build/smoke/promote order")
	}

	buildJob := workflow[buildStart:smokeStart]
	if !strings.Contains(buildJob, "type=raw,value=build-${{ github.ref_name }}") {
		t.Fatal("build job must push only a staging tag")
	}
	if strings.Contains(buildJob, "type=ref,event=tag") || strings.Contains(buildJob, "${TARGET_IMAGE}:${RELEASE_TAG}") {
		t.Fatal("build job can write the exact release tag before smoke tests")
	}

	promoteJob := workflow[promoteStart:]
	recheck := mustIndex(t, promoteJob, "Recheck immediately before the first immutable release write")
	exactWrite := mustIndex(t, promoteJob, "--tag \"${TARGET_IMAGE}:${RELEASE_TAG}\"")
	exactVerify := mustIndex(t, promoteJob, "Exact ${RELEASE_TAG} digest does not match the tested image")
	channelStep := mustIndex(t, promoteJob, "- name: Promote verified release channel")
	channelWrite := mustIndex(t, promoteJob[channelStep:], "--tag \"${TARGET_IMAGE}:${channel}\"") + channelStep
	if !(recheck < exactWrite && exactWrite < exactVerify && exactVerify < channelStep && channelStep < channelWrite) {
		t.Fatal("exact tag must be rechecked, written, and verified before channel promotion")
	}
}

func mustIndex(t *testing.T, value, marker string) int {
	t.Helper()
	index := strings.Index(value, marker)
	if index < 0 {
		t.Fatalf("missing workflow invariant marker %q", marker)
	}
	return index
}
