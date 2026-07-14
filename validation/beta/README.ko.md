# 공개 beta Webhook 검증

[English](README.md) | 한국어

이 evidence set은 서로 다른 공개 프로젝트의 실제 Webhook 설정 두 건을 AdmiTrace beta 전에 검증합니다. 검증 범위는 routing eligibility까지이며 각 프로젝트의 정책 판정, Webhook 응답, transport, patch, reinvocation은 평가하지 않습니다.

## 출처와 라이선스 게이트

두 후보는 Scenario 작성 전에 source gate를 통과했습니다. 둘 다 변경 불가능한 릴리스 커밋의 공개 프로젝트 소유 파일이며, 두 저장소 모두 Apache-2.0으로 소스를 공개합니다. 아래 저장소 링크와 해시는 이 엔지니어링 검증의 권한·재현성 근거이며 법률 자문이 아닙니다.

| 프로젝트 | 고정 출처 | SHA-256 | 라이선스 근거 |
| --- | --- | --- | --- |
| OPA Gatekeeper `v3.22.2` (`eda110bdaf2510288dccd73a1be4dd0c6442a4aa`) | [`deploy/gatekeeper.yaml`](https://raw.githubusercontent.com/open-policy-agent/gatekeeper/eda110bdaf2510288dccd73a1be4dd0c6442a4aa/deploy/gatekeeper.yaml) | `72683f57fdfa4c34d4a892e5e6f457a5a7e533eba0293d781d53d08dd6614a5a` | [Apache-2.0 LICENSE](https://raw.githubusercontent.com/open-policy-agent/gatekeeper/eda110bdaf2510288dccd73a1be4dd0c6442a4aa/LICENSE), SHA-256 `c71d239df91726fc519c6eb72d318ec65820627232b2f796219e87dcf35d0ab4` |
| Istio `1.30.0` (`badd809ed7d57954d4c16e12e75e15a7722a7b96`) | [`mutatingwebhook.yaml`](https://raw.githubusercontent.com/istio/istio/badd809ed7d57954d4c16e12e75e15a7722a7b96/manifests/charts/istio-control/istio-discovery/templates/mutatingwebhook.yaml) | `0d0c1fdf2f607ce2eed45e68a37ed31e7301fc99f4853c833c91e1a2ab559223` | [Apache-2.0 LICENSE](https://raw.githubusercontent.com/istio/istio/badd809ed7d57954d4c16e12e75e15a7722a7b96/LICENSE), SHA-256 `9fa6e54dafda853bb3cdc01486b677a55102f0d488282a85ba6e426d9125f8c5` |

Istio 템플릿은 동일 릴리스의 [`values.yaml`](https://raw.githubusercontent.com/istio/istio/badd809ed7d57954d4c16e12e75e15a7722a7b96/manifests/charts/istio-control/istio-discovery/values.yaml), SHA-256 `25a8185104caeeca0b8224fc2c78a7eea2bed9673f29fe99cf2a1c5bc72046e6`에서 렌더링했습니다. 렌더링에는 기본 empty revision, `enableNamespacesByDefault: false`, `reinvocationPolicy: Never`, `/inject` path를 사용했습니다.

Gatekeeper의 고정 [NOTICE](https://raw.githubusercontent.com/open-policy-agent/gatekeeper/eda110bdaf2510288dccd73a1be4dd0c6442a4aa/NOTICE), SHA-256 `7d4302ff15270639b7f8d25e9dbfb988b6d4075a36d0e47fdb0d4a91661b18d7`에 있는 “Gatekeeper, Copyright 2018-2020 The Gatekeeper Authors” attribution도 이 문서에 유지했습니다.

## 변환과 익명화

- 공개 Webhook 이름, 순서, rule, selector, failure policy, side effects, admission review version, 관련 default를 유지했습니다.
- 생략된 routing field는 YAML에서도 생략된 상태로 두며 decode와 API 등록 시 동일한 Kubernetes `1.36.2` default로 해석됩니다.
- 클러스터 생성 metadata, CA bundle, release-manager label, 무관한 workload object는 복사하지 않았습니다.
- request UID, user, namespace, name, label, image, resource body는 합성 값입니다. 보존해야 할 private project 또는 customer identifier는 없습니다.
- conformance oracle은 각 `clientConfig`만 전용 loopback TLS recorder로 바꿉니다. production evaluator는 여전히 네트워크에 접근하지 않습니다.
- Gatekeeper fixture는 `gatekeeper-validating-webhook-configuration`의 validating webhook 2개를 사용합니다. Istio fixture는 default-revision sidecar injector branch 4개를 렌더링합니다.

재현 가능한 Scenario는 다음과 같습니다.

- [`gatekeeper-v3.22.2-validating.yaml`](scenarios/gatekeeper-v3.22.2-validating.yaml)
- [`istio-1.30.0-mutating.yaml`](scenarios/istio-1.30.0-mutating.yaml)

## 재현

저장소 루트에서 `KUBEBUILDER_ASSETS`를 고정된 Kubernetes `1.36.2` envtest 바이너리로 지정한 뒤 실행합니다.

```sh
export KUBEBUILDER_ASSETS=/path/to/k8s/1.36.2-platform-arch
./hack/test-beta-validation.sh
```

이 스크립트는 현재 CLI를 빌드하고, `admitrace test` text와 JSON flow를 모두 실행하며, machine-readable report를 fresh evaluator 결과와 대조한 뒤 Kubernetes API server oracle을 실행합니다.

오프라인 구간만 재현하려면 다음을 실행합니다.

```sh
go build -o /tmp/admitrace-beta ./cmd/admitrace
/tmp/admitrace-beta test validation/beta/scenarios
/tmp/admitrace-beta --output json test validation/beta/scenarios
go test -count=1 ./validation/beta
```

## 결과

canonical record는 [`report.json`](report.json)입니다.

| 프로젝트 | 설정 | Webhook 관찰 | Incomplete |
| --- | --- | --- | --- |
| Gatekeeper | Validating, Webhook 2개 | `validation.gatekeeper.sh`: `called` / `MATCH_CONDITIONS_TRUE`; ignore-label Webhook: `skipped` / `RULE_NO_MATCH` | `0/2` |
| Istio | Mutating, 렌더링 branch 4개 | namespace-label branch: `called` / `MATCH_CONDITIONS_TRUE`; 다른 branch 3개: `skipped` / `NAMESPACE_SELECTOR_NO_MATCH` | `0/4` |

전체 Webhook 결과 6건은 모두 determinate이고, called 2건, skipped 4건, incomplete 비율은 `0%`입니다. Kubernetes `1.36.2` envtest도 동일한 Webhook별 호출 패턴을 관찰했으므로 semantic mismatch count는 0입니다.

### Trace 이해 가능성

- Gatekeeper의 skipped 두 번째 Webhook은 `exactRules`에서 종료되며, `sourcePath`가 skip을 모호하게 두지 않고 해당 Webhook의 rule을 가리킵니다.
- Istio의 제외된 branch 3개는 의도적으로 `NAMESPACE_SELECTOR_NO_MATCH`를 공유합니다. 안정적인 Webhook 이름, index, path, namespace input summary로 렌더링된 branch를 식별할 수 있습니다.
- called branch는 selector와 exact rule을 지나 terminal `MATCH_CONDITIONS_TRUE`까지 이어지므로, 성공적인 원격 호출을 암시하지 않으면서 선택 순서를 보여 줍니다.

### Incomplete와 unsupported 검토

`indeterminate` 또는 `unsupported`인 Webhook 결과는 없으며, 필요한 Namespace fixture 누락도 없습니다. 정보성 `CAPABILITY_OUTSIDE_PROFILE` diagnostic은 의도적으로 지원하지 않는 경계를 계속 표시합니다.

- AdmissionReview negotiation과 HTTP/TLS transport는 오프라인 제품 결과의 일부가 아닙니다.
- Webhook response와 Gatekeeper policy 또는 Istio injection decision은 평가하지 않습니다.
- Istio 결과는 initial-snapshot eligibility만 의미하며 patch와 `reinvocationPolicy`는 시뮬레이션하지 않습니다.

이 diagnostic들은 요청된 snapshot routing 자체가 완전히 평가 가능하므로 routing 결과를 unsupported determination으로 바꾸지 않습니다.

## Scope disposition

이번 검증에서 beta 범위를 바꿀 요구는 발견되지 않았고 제품 동작도 추가하지 않았습니다. response evaluation, patch application, reinvocation, live-cluster capture, project adapter는 실제 사용자 수요가 확인될 때의 future-iteration 후보로만 남깁니다.
