# AdmiTrace

[English](README.md) | 한국어

AdmiTrace는 Kubernetes Admission Webhook의 호출 대상 판정을 오프라인에서 재현하고, 그 이유를 순서가 보존된 trace로 설명하는 도구입니다.

`ValidatingWebhookConfiguration`과 `MutatingWebhookConfiguration`을 동일한 request snapshot으로 평가하며, Kubernetes `1.36.2` 기본 동작을 최초 compatibility profile로 사용합니다.

> [!IMPORTANT]
> AdmiTrace의 `called`는 지원하는 routing pipeline에서 Webhook이 **호출 대상으로 선택되었다**는 뜻입니다. 실제 HTTP/TLS 요청이나 Webhook 응답 성공을 의미하지 않습니다.

## 현재 상태

현재 저장소에는 Scenario 계약부터 통합 snapshot evaluator까지의 판정 코어가 구현되어 있습니다. Canonical renderer와 사용자용 `version`, `explain`, `test` CLI 명령, Kubernetes envtest parity suite는 개발 중입니다.

따라서 지금은 라이브 클러스터 운영 도구나 완성된 CLI 제품이 아니라, 정확성과 parity 검증을 우선하는 개발 단계의 프로젝트입니다.

## 핵심 기능

- YAML/JSON Scenario strict decoding과 입력 검증
- Validating·Mutating Webhook의 공통 정규화
- Namespace selector와 object selector 평가
- operation, GVR, subresource, scope 기반 Exact rule matching
- 명시적 fixture를 사용하는 Equivalent fallback
- Kubernetes CEL 환경 기반 `matchConditions` 평가
- `failurePolicy` Fail·Ignore와 false 우선순위 처리
- Namespace, authorization, equivalence 외부 문맥의 fixture 재생
- 단계별 source path, reason code, pending·discarded·terminal 상태 trace
- 같은 snapshot에 대한 다중 Webhook 독립 평가와 입력 순서 보존
- live Kubernetes client와 outbound network가 없는 offline runtime

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
- Webhook kinds:
  - `ValidatingWebhookConfiguration`
  - `MutatingWebhookConfiguration`

`matchPolicy=Exact`는 profile 범위에서 직접 평가합니다. `Equivalent`는 Exact miss 이후 명시적으로 제공된 equivalence fixture가 있을 때만 판정을 계속합니다. Namespace와 authorization도 라이브 클러스터 조회 없이 fixture만 사용합니다.

## 비범위

현재 AdmiTrace는 다음 기능을 수행하지 않습니다.

- 실제 Admission Webhook HTTP/TLS 호출
- Webhook 응답의 allow/deny, status 또는 audit annotation 검증
- JSON Patch 적용과 Mutating Webhook patch chain 예측
- `reinvocationPolicy` 시뮬레이션
- 라이브 Namespace, authorization 또는 API discovery 조회
- `AdmissionReviewVersions` 협상과 transport 성공 보장
- timeout, 인증서, 네트워크 장애 또는 부하 테스트
- 지원이 검증되지 않은 Kubernetes 버전의 근사 판정

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
go build -o ./build/admitrace ./cmd/admitrace
./build/admitrace --help
```

Kubernetes dependency boundary도 함께 검증할 수 있습니다.

```bash
./hack/verify-dependencies.sh
```

## 아키텍처

```text
cmd/admitrace             CLI entrypoint
internal/cli              Cobra command boundary
internal/scenario         Scenario decode, defaults, validation
internal/normalize        Webhook and request normalization
internal/fixture          Namespace, equivalence, authorization fixtures
internal/compat/kube136   Kubernetes 1.36 compatibility adapters
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

## 로드맵

- Canonical JSON과 text renderer
- `version`, `explain`, `test` CLI 명령
- 입력 제한, redaction, fuzz, offline guardrail
- Kubernetes `1.36.2` envtest oracle과 parity matrix
- 사용자 문서, 실제 Webhook 사례 검증, v0.1 release readiness

