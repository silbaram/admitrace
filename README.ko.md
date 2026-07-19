# AdmiTrace

[English](README.md) | 한국어

AdmiTrace는 Kubernetes Admission Webhook의 호출 대상 판정을 재현하고 그 이유를 순서가 보존된 trace로 설명합니다. 재생 가능한 Scenario와 일반 Kubernetes resource YAML을 모두 받으며, resource mode는 기본 오프라인이고 필요할 때만 명시적 GET-only context hydration을 사용합니다.

`ValidatingWebhookConfiguration`과 `MutatingWebhookConfiguration`을 동일한 request snapshot으로 평가하며, Kubernetes `1.36.2` 기본 동작을 최초 compatibility profile로 사용합니다.

> [!IMPORTANT]
> AdmiTrace의 `called`는 지원하는 routing pipeline에서 Webhook이 **호출 대상으로 선택되었다**는 뜻입니다. 실제 HTTP/TLS 요청이나 Webhook 응답 성공을 의미하지 않습니다.

## 설치

Go를 사용해 태그가 지정된 릴리스를 설치합니다.

```bash
go install github.com/silbaram/admitrace/cmd/admitrace@v0.1.2
```

`$(go env GOPATH)/bin` 또는 `GOBIN`이 `PATH`에 포함돼 있는지 확인한 뒤 설치 결과를 검증합니다.

```bash
admitrace version
admitrace --help
```

릴리스 소스: [`v0.1.2`](https://github.com/silbaram/admitrace/tree/v0.1.2)

## 문서

- [빠른 시작](docs/quickstart.ko.md): single/multi-document resource 오프라인 실행, 제한적 hydration, snapshot export, 기존 Scenario CI 검사
- [Scenario·manifest adapter·결과 레퍼런스](docs/reference.ko.md): schema, provenance, GET-only hydration, SnapshotPolicy, reason code, 종료 코드와 명시적 비범위
- [공개 beta 검증](validation/beta/README.ko.md): Gatekeeper·Istio 고정 사례의 라이선스 출처, CLI 결과와 Kubernetes `1.36.2` oracle 근거
- [v0.1 릴리스 준비 검증](docs/release-readiness.ko.md): pin, unit·fuzz test, standalone smoke, parity, conformance, beta 근거와 문서 완료를 한 번에 검사하는 fail-closed 명령

## 현재 상태

현재 저장소에는 오프라인 판정 코어, universal manifest adapter, 생성된 Kubernetes `1.36.2` built-in catalog, opt-in GET-only hydration, exact-copy Scenario snapshot, canonical text·JSON renderer와 pinned envtest parity gate가 구현되어 있습니다.

AdmiTrace는 라이브 클러스터 운영 도구가 아닙니다. Hydration은 사용자가 명시한 한 번의 설명에 필요한 좁은 문맥만 읽으며 cluster를 계속 audit·observe하지 않습니다.

## 핵심 기능

- YAML/JSON Scenario strict decoding과 입력 검증
- 기존 Scenario와 raw Kubernetes resource를 구분하는 universal `-f/--file`
- 1-based provenance를 보존하는 single/multi-document, stdin, resource-directory 처리
- 생성된 `1.36.2` built-in catalog 기반 exact GVK→GVR/scope 해석
- version/discovery, WebhookConfiguration LIST, 필요한 Namespace GET만 허용하는 explicit-context hydration
- Configuration·Namespace file-first fallback과 명시적 admission identity flag
- Exact-copy-or-refuse Scenario snapshot export와 오프라인 재생
- Validating·Mutating Webhook의 공통 정규화
- Namespace selector와 object selector 평가
- operation, GVR, subresource, scope 기반 Exact rule matching
- 명시적 fixture를 사용하는 Equivalent fallback
- Kubernetes CEL 환경 기반 `matchConditions` 평가
- `failurePolicy` Fail·Ignore와 false 우선순위 처리
- Namespace, authorization, equivalence 외부 문맥의 fixture 재생
- 단계별 source path, reason code, pending·discarded·terminal 상태 trace
- 같은 snapshot에 대한 다중 Webhook 독립 평가와 입력 순서 보존
- `--context`를 명시하지 않으면 client 생성과 network access 0건

## 판정 모델

평가 결과는 `determination`과 선택적 `outcome`을 분리합니다.

| Determination | 의미 | Outcome |
| --- | --- | --- |
| `determinate` | 지원 범위 안에서 판정 완료 | 필수 |
| `indeterminate` | 필요한 fixture 문맥이 없음 | 없음 |
| `unsupported` | compatibility profile 밖의 의미론이 필요함 | 없음 |

`determinate`일 때만 다음 outcome 중 하나가 설정됩니다.

| Outcome | 의미 |
| --- | --- |
| `called` | snapshot routing pipeline에서 호출 대상으로 선택됨 |
| `skipped` | selector, rule 또는 CEL 조건으로 호출 대상에서 제외됨 |
| `rejected-before-call` | 호출 전 평가 오류가 요청 거부로 귀결됨 |

Validating 평가는 `snapshot-routing`, Mutating 평가는 `mutating-initial-snapshot-eligibility` phase를 사용합니다. Mutating 결과는 이전 Webhook patch나 reinvocation이 적용되기 전의 초기 eligibility입니다.

## 지원 범위

- Scenario API: `admitrace.io/v1alpha1`
- Result schema: `admitrace.result/v1alpha1`
- Kubernetes profile: `kubernetes-1.36.2-defaults`
- Kubernetes modules: `v0.36.2`
- Configuration API: `admissionregistration.k8s.io/v1`
- Raw resource operation: `CREATE`만 지원
- Resource explanation schema: `admitrace.manifest-explanation/v1alpha1`
- Hydration boundary: explicit context, 정확한 server `v1.36.2`, HTTP GET only
- Webhook kinds:
  - `ValidatingWebhookConfiguration`
  - `MutatingWebhookConfiguration`

`-f/--file`은 universal input입니다. 단일 `admitrace.io/v1alpha1` Scenario는 기존 출력을 유지하고, 그 밖의 file/stdin/directory는 resource mode를 사용합니다. 오프라인 resource는 생성된 built-in catalog에 있어야 하며 CRD는 exact-version context의 verified discovery가 필요합니다. Plural·scope는 추측하지 않습니다.

Hydration은 kubeconfig current context를 암묵적으로 선택하지 않습니다. Version, discovery, WebhookConfiguration LIST, 필요한 Namespace에 대한 GET만 허용합니다. `--webhook-config`·`--namespace-object` 파일이 있으면 해당 cluster read를 생략합니다. Kubeconfig identity는 admission identity가 아니며 `--user`, `--group`, `--user-uid`, `--user-extra`만 `request.userInfo`를 구성합니다.

## 비범위

현재 AdmiTrace는 다음 기능을 수행하지 않습니다.

- 실제 Admission Webhook HTTP/TLS 호출
- Webhook 응답의 allow/deny, status 또는 audit annotation 검증
- JSON Patch 적용과 Mutating Webhook patch chain 예측
- `reinvocationPolicy` 시뮬레이션
- 암묵적 current-context 접근, cluster-wide audit, watch/informer 또는 API 변경
- SubjectAccessReview 등 POST 기반 permission preflight
- `AdmissionReviewVersions` 협상과 transport 성공 보장
- kube-apiserver의 dry-run 호출 전 거부 시뮬레이션. 지원하지 않는 `dryRun`·`sideEffects` 조합은 `unsupported`로 남음
- AdmissionRequest history 또는 kubeconfig credential을 request identity로 capture
- timeout, 인증서, 네트워크 장애 또는 부하 테스트
- 지원이 검증되지 않은 Kubernetes 버전의 근사 판정
- 안정적인 public Go API, JUnit XML 또는 프로젝트 전용 adapter 제공

## 개발 환경

- Go `1.26.0`
- Go toolchain `1.26.5`
- Kubernetes staging modules `v0.36.2`
- Cobra `v1.10.2`

저장소를 빌드하고 테스트하려면 다음 명령을 사용합니다.

```bash
git clone https://github.com/silbaram/admitrace.git
cd admitrace

go test ./...
go vet ./...
mkdir -p ./build
go build -o ./build/admitrace ./cmd/admitrace
./build/admitrace --help
./build/admitrace explain --file docs/examples/validating.yaml
./build/admitrace explain --resource docs/manifest-examples/resource.yaml \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml
./build/admitrace --output json test docs/examples
```

Kubernetes dependency boundary도 함께 검증할 수 있습니다.

```bash
./hack/verify-dependencies.sh
```

### 로컬 안전 제한

AdmiTrace는 1 MiB를 초과하거나 container 중첩이 100단계를 넘는 Scenario를
거부합니다. 한 번의 `admitrace test` 실행에서는 탐색된 Scenario 문서를 최대
1,000개까지 처리합니다. 제한 초과는 안정적인 invalid-input 진단이며 Webhook
`failurePolicy`로 해석하지 않습니다.

Guardrail 검증은 다음 두 그룹을 각각 독립적으로 실행할 수 있습니다.

```bash
./hack/test-resource-limits-fuzz.sh
./hack/test-redaction-offline-determinism.sh
```

첫 그룹은 입력·문서·CEL 제한과 seed가 있는 decoder·fixture fuzz target을
검증합니다. 두 번째 그룹은 민감 값 redaction, canonical byte equality, offline
실행과 production runtime 경계를 검증합니다.

## 아키텍처

```text
cmd/admitrace             CLI entrypoint
internal/cli              Cobra command boundary
internal/scenario         Scenario decode, defaults, validation
internal/normalize        Webhook and request normalization
internal/fixture          Namespace, equivalence, authorization fixtures
internal/compat/kube136   Kubernetes 1.36 compatibility adapters
internal/resourcecatalog  generated built-in discovery catalog contract
internal/manifest         raw manifest decode, identity, Scenario builder
internal/adapter          file-first context completeness와 hydration resolver
internal/hydration        explicit exact-version GET-only Kubernetes reader
internal/snapshot         exact-copy-or-refuse Scenario bundle writer
internal/routing          Selectors, rules, pre-CEL orchestration
internal/matchcondition   CEL matchConditions evaluation
internal/evaluation       Integrated snapshot evaluator
internal/contract         Scenario, result, trace, diagnostic contracts
```

Kubernetes 버전 의존 코드는 `internal/compat/kube136` 뒤에 격리합니다. 판정 코어는 네트워크에 접근하거나 프로세스를 종료하지 않으며, CLI가 입출력과 종료 코드 정책을 소유합니다.

## 개발 원칙

- 넓은 기능 지원보다 Kubernetes parity와 설명 가능성을 우선합니다.
- 필요한 외부 문맥이 없으면 추측하지 않습니다.
- `indeterminate`, `unsupported`, Kubernetes evaluation error, invalid input을 구분합니다.
- trace와 canonical 결과에서 입력 순서와 결정론을 유지합니다.
- Secret payload와 authorization identity를 진단에 그대로 복제하지 않습니다.
- `k8s.io/kubernetes` root module을 production dependency로 사용하지 않습니다.

## 버전 정책

`kubernetes-1.36.2-defaults` profile은 해당 릴리스의 기본 feature gate를 사용하는 Kubernetes `1.36.2`만 의미합니다. 새 Kubernetes 버전은 별도 compatibility profile과 해당 정확한 버전의 parity 근거가 필요하며 기존 profile로 근사하지 않습니다.

## 라이선스

AdmiTrace는 Apache License 2.0으로 배포됩니다.
