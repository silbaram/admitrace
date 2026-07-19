# Scenario·결과 레퍼런스

[English](reference.md) | 한국어

현재 CLI가 지원하는 compatibility target은 Kubernetes `1.36.2`와 해당 릴리스의 기본 feature-gate 값뿐입니다.

## Scenario 계약

Scenario는 strict YAML 또는 JSON이며 다음 최상위 필드를 사용합니다.

| 필드 | 필수 | 계약 |
| --- | --- | --- |
| `apiVersion` | 예 | `admitrace.io/v1alpha1` |
| `kind` | 예 | `Scenario` |
| `metadata.name` | 예 | 안정적인 Scenario 식별자 |
| `compatibilityProfile` | 예 | `kubernetes-1.36.2-defaults`, `1.36.2`, `kubernetes-defaults`의 정확한 조합 |
| `configuration` | 예 | Validating 또는 Mutating configuration 중 정확히 하나 |
| `request` | 예 | 모든 Webhook이 공유하는 불변 AdmissionRequest snapshot |
| `externalContext` | 아니요 | Namespace, authorization, equivalence fixture |
| `expectations` | 아니요 | `admitrace test`가 비교하는 Webhook별 기대값 |

알 수 없는 필드와 중복 YAML·JSON key는 거부합니다. 문서 크기는 1 MiB, container 중첩은 100단계까지입니다.

### 지원 profile 정책

```yaml
compatibilityProfile:
  id: kubernetes-1.36.2-defaults
  kubernetesVersion: 1.36.2
  featureGatePolicy: kubernetes-defaults
```

이 profile은 LTS 범위나 최소 버전이 아니라 Kubernetes `1.36.2` 기본 동작의 정확한 식별자입니다. 새 Kubernetes 버전은 해당 버전 API server parity matrix를 통과한 격리된 compatibility 구현과 새 profile로만 추가합니다. 기존 profile의 의미를 바꾸거나 새 버전을 `1.36.2`로 근사하지 않습니다.

### Configuration과 request

Configuration은 `admissionregistration.k8s.io/v1` 형태이며 Webhook 순서, rules, operation, GVR, subresource, scope, namespace·object selector, `Exact`·`Equivalent`, `matchConditions`, `failurePolicy`, `sideEffects`를 평가합니다. 생략된 routing 값에는 `failurePolicy: Fail`, `matchPolicy: Equivalent`, rule `scope: "*"` 기본값이 적용됩니다.

`request.dryRun`이 true일 때 `sideEffects: None`과 `NoneOnDryRun`은 eligibility를 계속 평가할 수 있습니다. 그 밖의 명시된 `sideEffects` 값은 `CAPABILITY_OUTSIDE_PROFILE`을 가진 `unsupported`입니다. kube-apiserver의 dry-run 호출 전 거부나 transport는 시뮬레이션하지 않습니다.

Request의 주요 필드는 `kind`, `resource`, `subResource`, 선택적 원본 `requestKind`·`requestResource`·`requestSubResource`, `operation`, `scope`, `name`, `namespace`, `userInfo`, `dryRun`, `object`, `oldObject`, `options`입니다. 모든 Webhook은 동일한 snapshot에서 configuration 순서대로 독립 평가됩니다.

### 외부 fixture

기존 Scenario 경로는 항상 오프라인입니다.

- `namespace`: namespaced request의 non-empty namespace selector에 필요한 정확한 Namespace
- `authorization`: resource 또는 non-resource query와 `allow`, `deny`, `no-opinion`, `error` verdict
- `equivalence`: request GVR/subresource와 순서가 있는 equivalent GVR·GVK·subresource 후보

필요한 fixture가 없으면 추측하지 않고 `indeterminate`가 됩니다. profile이 표현할 수 없는 변환이면 `unsupported`입니다.

### Expectations

Expectation은 설정된 `webhookName`과 `determination`을 필수로 가지며 `outcome`, `terminalReasonCode`를 선택적으로 비교합니다. `indeterminate`와 `unsupported` expectation에는 outcome을 두지 않습니다.

## Manifest adapter와 제한적 hydration

`admitrace explain`은 일반 Kubernetes YAML·JSON도 입력으로 받습니다. `-f/--file`은 universal input flag입니다.

- 단일 `admitrace.io/v1alpha1` `Scenario` 문서는 기존 경로와 `admitrace.result/v1alpha1` 결과를 그대로 사용합니다.
- 그 밖의 파일·stdin 문서는 resource mode로 들어갑니다.
- Multi-document stream과 directory는 resource 전용이며 Scenario가 섞이면 거부합니다.
- `--resource`는 resource mode를 명시하며 `--file`과 함께 사용할 수 없습니다.

Resource와 WebhookConfiguration은 논리 filename/source 종류와 1-based document index를 보존합니다. Directory는 filename lexical 순서, 그 안에서는 문서 순서로 처리합니다. N번째 문서가 잘못되면 평가 전에 해당 source/index를 보고하며, 전체가 처리된 것처럼 보이는 partial output은 만들지 않습니다.

Resource mode는 `CREATE`만 지원합니다. 입력 resource의 object/name/namespace와 정확한 GVK→GVR/scope를 유도합니다. 오프라인에서는 생성·커밋된 Kubernetes `1.36.2` built-in catalog만 사용하고, 알 수 없는 GVK·CRD의 plural이나 scope를 추측하지 않습니다.

### 문맥 source 우선순위와 안전 경계

Hydration은 `--context <name>`을 명시할 때만 활성화됩니다. `--kubeconfig`는 선택 사항이지만 `--context`와 함께만 유효하며, default/current context를 암묵적으로 선택하지 않습니다. 연결 대상은 정확히 `v1.36.2`, major/minor `1.36`이어야 합니다. 다른 patch/minor, `v` 누락, vendor/build suffix는 discovery나 configuration 수집 전에 profile mismatch로 중단됩니다.

보호된 client가 허용하는 HTTP GET은 다음뿐입니다.

1. `GET /version`
2. Kubernetes API discovery GET
3. ValidatingWebhookConfiguration·MutatingWebhookConfiguration LIST에 해당하는 HTTP GET
4. 선택된 namespace selector에 필요한 resource별 Namespace GET

POST·PUT·PATCH·DELETE, SubjectAccessReview, server-side dry-run, watch/informer, kubectl subprocess는 허용하지 않습니다. Read가 forbidden/unavailable이면 `contextCompleteness`에 기록하고 match나 skip으로 추측하지 않습니다.

명시적 파일은 cluster read보다 우선합니다. `--webhook-config`는 두 configuration LIST를 생략하고 `--namespace-object`는 Namespace GET을 생략합니다. CRD GVK를 해석하려고 `--context`를 쓴 경우 verified discovery는 계속 수행합니다.

Kubeconfig user/certificate/token은 API 연결만 식별하며 `request.userInfo`로 복사하지 않습니다. Admission identity는 `--user`, 반복 가능한 `--group`, `--user-uid`, 반복 가능한 `--user-extra key=value`로만 만듭니다. 평가에 도달한 condition이 identity, authorization, equivalence, Namespace 문맥을 요구하지만 없으면 fail-closed 결과를 유지합니다.

### Manifest 설명 envelope

Resource mode는 resource 하나마다 `admitrace.manifest-explanation/v1alpha1` object 하나를 출력합니다. 여러 resource는 순서가 보존된 JSON array 또는 text 문서 sequence가 됩니다. 각 object에는 다음이 포함됩니다.

- resource source와 1-based document provenance
- declared 또는 verified profile 상태
- `configuration`, `discovery`, `namespace`, `identity`, `equivalence`, `authorization` completeness
- source-indexed adapter diagnostics
- configuration별 순서가 보존된 기존 `admitrace.result/v1alpha1` 평가

필수 completeness가 missing·forbidden·unsupported이거나 evaluator 결과가 incomplete이면 종료 코드 `3`입니다. 중첩 결과의 `called`는 여전히 routing에서 호출 대상으로 선택됐다는 뜻일 뿐입니다.

### SnapshotPolicy

`--snapshot-out <directory>`는 resource/configuration 순서쌍마다 `rrrr-cccc.yaml` 이름의 canonical Scenario를 기록합니다. 대상은 존재하지 않거나 비어 있어야 하며 staging 후 atomic하게 게시합니다. Directory/file mode는 `0700`/`0600`입니다.

정책은 exact-copy-or-refuse입니다.

- core/v1 `Secret`은 bundle을 하나도 게시하기 전에 항상 거부합니다.
- 명시적 admission `UserInfo`와 사용자가 제공한 일반 resource·CRD payload는 정확한 재생을 위해 변경 없이 저장합니다.
- Field redaction을 하지 않으며, 임의 custom-resource field 안의 generic secret detection을 보장하지 않습니다.
- Kubeconfig bytes, bearer token, client certificate/key, API server URL, 선택한 context/cluster identity, 자동 인증된 connection user는 저장하지 않습니다.

Snapshot 전에 입력을 검토해야 합니다. Secret이 아닌 resource에도 민감한 사용자 데이터가 있을 수 있으며 exact replay는 이를 의도적으로 보존합니다.

## 결과 계약

기존 Scenario의 JSON 결과 schema는 `admitrace.result/v1alpha1`입니다. Resource mode는 위 manifest envelope 안에 이 결과를 변경 없이 중첩합니다. 최상위에는 `scenarioId`, `compatibilityProfile`, `evaluationPhase`, `configurationKind`, 순서가 있는 `webhooks`, `diagnostics`가 있습니다. 각 Webhook 결과에는 `webhookName`, `webhookIndex`, `configurationKind`, `sourcePath`, `determination`, 선택적 `outcome`, `trace`, `diagnostics`가 있습니다.

| Determination | Outcome | 의미 |
| --- | --- | --- |
| `determinate` | 필수 | 지원 계약 안에서 평가 완료 |
| `indeterminate` | 없음 | 필요한 fixture가 없어서 추측하지 않음 |
| `unsupported` | 없음 | 필요한 의미론이 profile 범위 밖임 |

Determinate outcome은 `called`, `skipped`, `rejected-before-call`입니다. `called`는 호출 대상으로 선택되었다는 뜻이며 실제 HTTP 요청이나 Webhook allow 응답을 의미하지 않습니다.

Trace는 `stage`, `sourcePath`, `sequence`, redacted `inputSummary`, `result`, `reasonCode`, `pending`, `discarded`, `terminal`을 가집니다. 자동화는 message가 아니라 다음 안정적 reason code를 사용해야 합니다.

```text
ADMISSION_CONFIGURATION_EXCLUDED  AUTHORIZATION_CONTEXT_MISSING
CEL_AUTHORIZATION_ERROR           CEL_COMPILE_ERROR
CEL_COST_BUDGET_EXCEEDED          CEL_RUNTIME_ERROR
CAPABILITY_OUTSIDE_PROFILE        EVALUATION_PROBLEM_DISCARDED
EVALUATION_PROBLEM_PENDING        EQUIVALENCE_CONTEXT_MISSING
IDENTITY_CONTEXT_MISSING          INTERNAL_ERROR
INVALID_INPUT
KUBERNETES_EVALUATION_ERROR       MATCH_CONDITIONS_TRUE
MATCH_CONDITION_FALSE             MATCH_CONDITION_TRUE
NAMESPACE_CONTEXT_MISSING         NAMESPACE_SELECTOR_MATCH
NAMESPACE_SELECTOR_NO_MATCH       OBJECT_SELECTOR_MATCH
OBJECT_SELECTOR_NO_MATCH          RULE_MATCH
RULE_NO_MATCH                     STAGE_NOT_RUN
```

각 코드의 의미는 [영문 reason code 표](reference.md#trace)에 정리되어 있습니다.

## CLI와 종료 코드

```text
admitrace [--output text|json] explain --file <path|directory|-> [resource-mode flags]
admitrace [--output text|json] explain --resource <path|directory|-> [resource-mode flags]
admitrace [--output text|json] test <path>...
admitrace [--output text|json] version
```

`--output` 기본값은 text입니다. Resource-mode flag에는 `--webhook-config`, `--namespace-object`, `--context`, `--kubeconfig`, 명시적 identity flag, `--operation CREATE`, `--snapshot-out`이 있습니다. `test`는 Scenario 전용입니다. JSON test report schema는 `admitrace.test/v1alpha1`이며 ordered `fixtures`와 `summary`를 제공합니다. 한 번에 발견하는 문서는 최대 1,000개입니다.

| 코드 | 의미 | 예시 |
| --- | --- | --- |
| `0` | determinate 설명 또는 expectation 모두 일치 | `admitrace test docs/examples` |
| `1` | expectation mismatch | 기대 `called`를 `skipped`로 변경 |
| `2` | 사용법, schema, 파일 또는 resource limit 오류 | `admitrace explain`에서 `--file` 생략 |
| `3` | explain incomplete 또는 expectation 없는 incomplete test | equivalence fixture 없는 Equivalent fallback |
| `4` | 내부 invariant, render 또는 output write 오류 | CLI process test에서 실패 writer를 주입하는 예제 |

여러 fixture의 우선순위는 internal error, invalid input, mismatch, incomplete, success 순서입니다. 정확히 기대한 `indeterminate`·`unsupported`는 `0`입니다.

영문 레퍼런스의 [재현 가능한 종료 코드 명령](reference.md#exit-codes)은 포함된 fixture를 사용해 사용자가 만들 수 있는 `0`부터 `3`까지 확인합니다. `4`는 지원 사용자 흐름으로 일부러 만드는 값이 아니라 CLI test에서 실패 writer를 주입해 검증하는 보호 코드입니다. 정상 CI에서는 `set +e`를 사용하지 않아 nonzero status가 작업을 실패시키게 합니다.

## Mutating 제한과 명시적 비범위

Mutating phase `mutating-initial-snapshot-eligibility`는 최초 snapshot의 eligibility만 설명합니다. 반환 patch 적용, 다음 Webhook에 변경 object 전달, reinvocation과 최종 mutation chain 예측은 하지 않습니다.

현재 제품은 다음을 하지 않습니다.

- AdmissionReview HTTP/TLS 호출
- Webhook response allow/deny, status, warning, audit annotation, patch 평가
- transport, certificate, timeout, `AdmissionReviewVersions` 협상
- patch ordering, 적용, `reinvocationPolicy` 시뮬레이션
- 암묵적/current kubeconfig context 선택, cluster watch, cluster 전체 audit/history 수집
- kubeconfig에서 admission identity 추론 또는 SubjectAccessReview로 live authorization 조회
- Kubernetes API 변경 요청, server-side dry-run, watch, informer 사용
- 클러스터에서 라이브 AdmissionRequest snapshot을 캡처하는 명령
- JUnit XML 출력
- 프로젝트 전용 adapter 또는 upstream 통합
- 안정적인 public Go API 보장
- 검증하지 않은 Kubernetes 버전 근사

명시적 hydration은 GET-only로 제한된 input adapter이며 observability나 request capture가 아닙니다. 별도 envtest suite의 실제 로컬 Kubernetes `1.36.2` API server는 이 경계와 parity를 검증하는 개발 oracle일 뿐 production runtime에 포함되지 않습니다.
