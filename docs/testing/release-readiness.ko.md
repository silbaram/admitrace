# Manifest Adapter 릴리스 준비 검증

[English](release-readiness.md) | 한국어

릴리스 채널은 기존 오프라인 Scenario evaluator와 universal manifest adapter, 명시적 GET-only hydration을 함께 제공합니다. 깨끗한 checkout에서 고정된 Go `1.26.5` toolchain, 캐시된 module dependency와 정확한 Kubernetes `1.36.2` envtest binary를 준비하고 다음 명령을 실행합니다.

```sh
export KUBEBUILDER_ASSETS=/path/to/k8s/1.36.2-platform-arch
GO_BIN=/path/to/go1.26.5/bin/go ./hack/verify-release-readiness.sh
```

각 fuzz target의 기본 2초 실행 시간은 `ADMITRACE_FUZZTIME`으로 바꿀 수 있습니다. 결정론적 parity JSON을 보존하려면 `PARITY_REPORT`에 경로를 지정하며 상대 경로는 저장소 root 기준으로 해석됩니다. 지정하지 않으면 임시 report를 사용합니다.

아래 항목이 모두 통과해야 명령이 종료 코드 0을 반환합니다.

## 자동 체크리스트

- [x] root unit, golden, CLI process, resource-limit, deterministic-output, offline manifest E2E, hydration-security, SnapshotPolicy와 legacy-path test 통과 및 이름이 고정된 sentinel 실행 확인
- [x] 필수 fuzz target 두 개가 선택 가능하고 실제 실행됨
- [x] Go language `1.26.0`, toolchain `go1.26.5`, Cobra `v1.10.2`, Kubernetes module `v0.36.2`, envtest `v0.24.1`, control-plane `1.36.2` pin 확인
- [x] production dependency graph에서 `k8s.io/kubernetes` root module, envtest, controller-runtime 부재 확인. Client/network 코드는 `internal/hydration`으로 격리하고 transport·정확한 요청 surface를 GET-only로 독립 audit
- [x] 생성된 Kubernetes `1.36.2` built-in resource catalog가 drift 없이 재생성되고 exact envtest discovery와 일치함
- [x] standalone `admitrace` binary로 `version`, 기존 Validating·Mutating `explain`, directory `test`, 오프라인 single/multi/directory manifest 예제와 snapshot replay 검증
- [x] standalone JSON legacy·multi-manifest `explain`과 `test` 반복 결과가 byte 단위로 동일함
- [x] determinate parity 29건 모두 Kubernetes `1.36.2` 독립 근거 보유: API server 직접 관찰 21건과 공식 matcher differential 8건, incomplete contract 4건은 별도 유지
- [x] parity report가 oracle 종류별 정확한 coverage와 semantic·differential mismatch 0건을 기록하며 reviewed golden trace 단독으로는 통과 불가
- [x] 필수 envtest conformance가 exact `1.36.2` catalog, CRD discovery/scope, status-subresource routing, 실제 RBAC denial, 제한된 configuration/Namespace hydration, canonical replay를 증명함
- [x] 전체 conformance suite와 공개 beta 프로젝트 두 사례가 고정된 로컬 API server에서 통과함
- [x] 실행 가능한 help·문서가 universal `-f`, 명시적 `--resource`, source/document provenance, `CREATE` only, exact-version GET-only hydration, explicit identity, offline CRD 거부, SnapshotPolicy와 routing-only `called`를 설명함
- [x] 모든 quickstart manifest 명령을 커밋된 예제와 기대 output schema에 대해 이 gate가 실행함

envtest API server, loopback TLS recorder와 Kubernetes 공식 matcher differential은 릴리스 검증에서만 사용하는 개발 oracle입니다. standalone 제품은 context를 명시했을 때만 문서화된 GET surface로 cluster를 읽으며 Webhook transport는 수행하지 않습니다. 검증 통과가 제품 범위를 response 평가, patch 적용, reinvocation, live AdmissionRequest capture 또는 프로젝트별 adapter로 확장하지 않습니다.
