# 공개 beta Webhook 검증

[English](README.md) | 한국어

이 evidence set은 서로 다른 공개 프로젝트의 실제 Webhook 설정 두 건을 AdmiTrace beta 전에 검증합니다. 검증 범위는 routing eligibility까지이며 각 프로젝트의 정책 판정, Webhook 응답, transport, patch, reinvocation은 평가하지 않습니다.

## 출처·라이선스와 변환

Scenario 작성 전에 다음 공개 Apache-2.0 출처와 태그 커밋을 확인했습니다.

- OPA Gatekeeper `v3.22.2`, commit `eda110bdaf2510288dccd73a1be4dd0c6442a4aa`: [`deploy/gatekeeper.yaml`](https://raw.githubusercontent.com/open-policy-agent/gatekeeper/eda110bdaf2510288dccd73a1be4dd0c6442a4aa/deploy/gatekeeper.yaml), SHA-256 `72683f57fdfa4c34d4a892e5e6f457a5a7e533eba0293d781d53d08dd6614a5a`; [LICENSE](https://raw.githubusercontent.com/open-policy-agent/gatekeeper/eda110bdaf2510288dccd73a1be4dd0c6442a4aa/LICENSE).
- Istio `1.30.0`, commit `badd809ed7d57954d4c16e12e75e15a7722a7b96`: [`mutatingwebhook.yaml`](https://raw.githubusercontent.com/istio/istio/badd809ed7d57954d4c16e12e75e15a7722a7b96/manifests/charts/istio-control/istio-discovery/templates/mutatingwebhook.yaml), SHA-256 `0d0c1fdf2f607ce2eed45e68a37ed31e7301fc99f4853c833c91e1a2ab559223`; [LICENSE](https://raw.githubusercontent.com/istio/istio/badd809ed7d57954d4c16e12e75e15a7722a7b96/LICENSE).
- Istio 렌더링 기본값은 동일 릴리스의 [`values.yaml`](https://raw.githubusercontent.com/istio/istio/badd809ed7d57954d4c16e12e75e15a7722a7b96/manifests/charts/istio-control/istio-discovery/values.yaml), SHA-256 `25a8185104caeeca0b8224fc2c78a7eea2bed9673f29fe99cf2a1c5bc72046e6`에서 확인했습니다.

Gatekeeper의 고정 [NOTICE](https://raw.githubusercontent.com/open-policy-agent/gatekeeper/eda110bdaf2510288dccd73a1be4dd0c6442a4aa/NOTICE), SHA-256 `7d4302ff15270639b7f8d25e9dbfb988b6d4075a36d0e47fdb0d4a91661b18d7`에 있는 “Gatekeeper, Copyright 2018-2020 The Gatekeeper Authors” attribution도 이 문서에 유지했습니다.

공개 Webhook 이름·순서·rule·selector·failure policy 등 routing 필드는 유지했습니다. 생략된 필드는 Scenario decode와 API 등록에서 동일한 Kubernetes `1.36.2` 기본값을 사용합니다. CA bundle, 클러스터 생성 metadata와 무관한 배포 값은 복사하지 않았고 request UID, 사용자, namespace, workload와 image는 모두 합성했습니다. oracle은 각 `clientConfig`만 loopback TLS recorder로 교체합니다. 이 검토는 엔지니어링 차원의 라이선스 적합성 확인이며 법률 자문이 아닙니다.

## 재현

```sh
export KUBEBUILDER_ASSETS=/path/to/k8s/1.36.2-platform-arch
./hack/test-beta-validation.sh
```

이 스크립트는 CLI text·JSON test, [`report.json`](report.json) 계약 검사, Kubernetes `1.36.2` envtest의 Webhook별 실제 호출 관찰을 실행합니다.

## 결과와 trace 피드백

- Gatekeeper 두 Webhook: `validation.gatekeeper.sh`는 `called` / `MATCH_CONDITIONS_TRUE`, ignore-label Webhook은 `skipped` / `RULE_NO_MATCH`입니다.
- Istio 네 Webhook: namespace-label branch만 `called` / `MATCH_CONDITIONS_TRUE`이고 나머지 세 branch는 `skipped` / `NAMESPACE_SELECTOR_NO_MATCH`입니다.
- 전체 6건이 determinate이고 called 2건, skipped 4건, indeterminate·unsupported determination 0건이므로 incomplete 비율은 `0%`입니다. envtest 관찰과 의미 불일치는 0건입니다.
- Gatekeeper trace의 `exactRules`와 Istio trace의 Webhook 이름·index·`sourcePath`·namespace input summary로 어느 branch가 선택되거나 제외됐는지 구분할 수 있었습니다.

`CAPABILITY_OUTSIDE_PROFILE` 정보 진단은 실제 HTTP/TLS·AdmissionReview 협상, Webhook 응답, Gatekeeper 정책 판정, Istio patch와 reinvocation이 평가되지 않았음을 계속 표시합니다. Mutating 결과는 최초 snapshot eligibility뿐입니다.

이번 검증에서 beta 범위를 바꿀 요구는 발견되지 않았고 제품 동작도 추가하지 않았습니다. response·patch·reinvocation·live snapshot·프로젝트 adapter는 실제 수요가 확인될 때의 후속 iteration 후보로만 남깁니다.
