# jarvis5.0 Reference Resources
**Version**: 5.0  
**Format**: Domain → Topic → URL + brief description  
**Note**: URLs are curated for quality and longevity. Prefer official docs and peer-reviewed sources.

---

## 1. Go Engineering

### Language & Standard Library
| Resource | URL | Notes |
|----------|-----|-------|
| Go Specification | https://go.dev/ref/spec | Authoritative language spec |
| Effective Go | https://go.dev/doc/effective_go | Idiomatic patterns |
| Go Memory Model | https://go.dev/ref/mem | Happens-before guarantees |
| Standard Library | https://pkg.go.dev/std | All packages |
| Go Generics Tutorial | https://go.dev/doc/tutorial/generics | Official generics intro |
| Go Blog | https://go.dev/blog | Authoritative deep dives |

### Tooling
| Resource | URL | Notes |
|----------|-----|-------|
| pprof Guide | https://pkg.go.dev/net/http/pprof | Profiling HTTP server |
| go test flags | https://pkg.go.dev/testing | Testing package docs |
| benchstat | https://pkg.go.dev/golang.org/x/perf/cmd/benchstat | Benchmark statistics |
| staticcheck | https://staticcheck.dev | Linter rules reference |
| golangci-lint | https://golangci-lint.run/usage/linters | All available linters |
| delve debugger | https://github.com/go-delve/delve/tree/master/Documentation | VSCode debugger |
| gopls user guide | https://github.com/golang/tools/blob/master/gopls/doc/user.md | LSP features |

### Concurrency & Performance
| Resource | URL | Notes |
|----------|-----|-------|
| Concurrency in Go (book) | https://www.oreilly.com/library/view/concurrency-in-go/9781491941294 | Best concurrency patterns |
| go-fuzz | https://github.com/dvyukov/go-fuzz | Property/fuzz testing |
| Profiling Go Programs | https://go.dev/blog/pprof | Flame graph interpretation |
| Data Race Detector | https://go.dev/doc/articles/race_detector | Race detection usage |
| Goroutine Leak Detection | https://github.com/uber-go/goleak | Leak detector |

### Key Packages for vProx
| Resource | URL | Notes |
|----------|-----|-------|
| net/http | https://pkg.go.dev/net/http | HTTP client/server |
| net/http/httputil | https://pkg.go.dev/net/http/httputil | ReverseProxy, DumpRequest |
| gorilla/websocket | https://pkg.go.dev/github.com/gorilla/websocket | WS library in use |
| golang.org/x/time/rate | https://pkg.go.dev/golang.org/x/time/rate | Token bucket limiter |
| oschwald/geoip2-golang | https://pkg.go.dev/github.com/oschwald/geoip2-golang | MMDB reader |
| go-toml v2 | https://pkg.go.dev/github.com/pelletier/go-toml/v2 | TOML config parsing |

---

## 2. Cosmos SDK / Blockchain

| Resource | URL | Notes |
|----------|-----|-------|
| Cosmos SDK Docs | https://docs.cosmos.network | Official docs (v0.50.x) |
| Cosmos SDK GitHub | https://github.com/cosmos/cosmos-sdk | Source + releases |
| CometBFT Docs | https://docs.cometbft.com | CometBFT v0.38 docs |
| IBC-go Docs | https://ibc.cosmos.network | IBC protocol docs |
| CosmWasm Docs | https://book.cosmwasm.com | CosmWasm smart contract book |
| Cosmos Developers | https://tutorials.cosmos.network | Developer tutorials |
| ADR Index | https://docs.cosmos.network/main/architecture | Architecture Decision Records |
| Cosmos SDK CHANGELOG | https://github.com/cosmos/cosmos-sdk/blob/main/CHANGELOG.md | Breaking changes tracker |
| CometBFT RPC | https://docs.cometbft.com/v0.38/rpc | Full RPC API |
| Cosmos REST API | https://cosmos.github.io/cosmos-rest-api | REST API reference |

### 2b. Cosmos SDK Hidden Gems (Proxy Intelligence — researched 2026-03-03)
| Resource | URL | Notes |
|----------|-----|-------|
| CometBFT config.go | https://github.com/cometbft/cometbft/blob/v0.38.x/config/config.go | `max_open_connections`, `max_subscription_clients`, `max_subscriptions_per_client` defaults |
| CometBFT WS handler | https://github.com/cometbft/cometbft/blob/v0.38.x/rpc/jsonrpc/server/ws_handler.go | WS ping period, write wait, buffer sizing |
| CometBFT RPC routes | https://github.com/cometbft/cometbft/blob/v0.38.x/rpc/core/routes.go | `Cacheable()` markers; expensive vs cheap route classification |
| Cosmos upgrade proto | https://github.com/cosmos/cosmos-sdk/blob/main/proto/cosmos/upgrade/v1beta1/query.proto | `/current_plan`, `/applied_plan/{name}`, `/module_versions` |
| IBC channel proto | https://github.com/cosmos/ibc-go/blob/main/proto/ibc/core/channel/v1/query.proto | `/channels` (no pagination — DoS risk), `packet_commitments`, `unreceived_packets` |
| gRPC reflection | https://github.com/cosmos/cosmos-sdk/tree/main/server/grpc/reflection | Reflection endpoint; leaks proto schema — consider auth-gate |
| CometBFT mempool | https://github.com/cometbft/cometbft/blob/v0.38.x/rpc/core/mempool.go | `num_unconfirmed_txs`, `broadcast_tx_commit` subscription blocking |
| Cosmos gov v1 proto | https://github.com/cosmos/cosmos-sdk/blob/main/proto/cosmos/gov/v1/query.proto | Proposal/votes/tally endpoints; votes can be unbounded |
| CometBFT ABCI query | https://github.com/cometbft/cometbft/blob/v0.38.x/rpc/core/abci.go | `prove=true` (merkle, expensive) vs `prove=false` (cheap) |

---

## 3. Statistics & Data Science

### Foundations
| Resource | URL | Notes |
|----------|-----|-------|
| Statistics with Python (scipy) | https://docs.scipy.org/doc/scipy/reference/stats.html | Statistical tests reference |
| Think Stats (free book) | https://greenteapress.com/thinkstats2/html | Practical statistics with Python |
| Statistical Inference (Casella & Berger) | https://www.cengage.com/c/statistical-inference-2e-casella | Authoritative statistical theory |
| Bayesian Data Analysis (BDA3) | http://www.stat.columbia.edu/~gelman/book | Gelman et al, free PDF |
| Causal Inference: The Mixtape | https://mixtape.scunning.com | Free online causal inference |
| Introduction to Statistical Learning | https://www.statlearning.com | ISLR, free PDF |
| Elements of Statistical Learning | https://hastie.su.domains/ElemStatLearn | ESL, free PDF |

### Experiment Design
| Resource | URL | Notes |
|----------|-----|-------|
| Trustworthy Online Controlled Experiments | https://www.cambridge.org/core/books/trustworthy-online-controlled-experiments | A/B testing at scale (Kohavi) |
| Sample size calculator | https://www.evanmiller.org/ab-testing/sample-size.html | Quick power analysis |
| Statsig A/B testing guide | https://statsig.com/blog/ab-testing-guide | Applied experiment design |

### Time Series
| Resource | URL | Notes |
|----------|-----|-------|
| Forecasting: Principles and Practice | https://otexts.com/fpp3 | Hyndman, free online book |
| statsmodels (Python) | https://www.statsmodels.org/stable/tsa.html | ARIMA, state space models |
| Prophet (Meta) | https://facebook.github.io/prophet | Time series forecasting library |

### Anomaly Detection
| Resource | URL | Notes |
|----------|-----|-------|
| Awesome Anomaly Detection | https://github.com/hoya012/awesome-anomaly-detection | Curated paper list |
| PyOD (Python) | https://pyod.readthedocs.io | Outlier detection library |
| Isolation Forest (paper) | https://ieeexplore.ieee.org/document/4781136 | Original IF algorithm |
| Z-score based detection | https://www.itl.nist.gov/div898/handbook/eda/section3/eda35h.htm | NIST statistical handbook |

---

## 4. Machine Learning

### Core Algorithms
| Resource | URL | Notes |
|----------|-----|-------|
| scikit-learn | https://scikit-learn.org/stable | Algorithm reference + guides |
| XGBoost | https://xgboost.readthedocs.io | Gradient boosting docs |
| LightGBM | https://lightgbm.readthedocs.io | Faster gradient boosting |
| Optuna | https://optuna.readthedocs.io | Hyperparameter optimization |

### Model Evaluation
| Resource | URL | Notes |
|----------|-----|-------|
| sklearn metrics | https://scikit-learn.org/stable/modules/model_evaluation.html | All metrics explained |
| Calibration guide | https://scikit-learn.org/stable/modules/calibration.html | Probability calibration |
| ML Testing (Google) | https://developers.google.com/machine-learning/testing-debugging | ML testing patterns |

### Deep Learning (Applied Reference)
| Resource | URL | Notes |
|----------|-----|-------|
| PyTorch | https://pytorch.org/docs | Reference docs |
| Andrej Karpathy NN Zero to Hero | https://karpathy.ai/zero-to-hero.html | Foundational neural net building |
| Transformers Explained | https://huggingface.co/docs/transformers | HuggingFace reference |

---

## 5. Observability & Performance

| Resource | URL | Notes |
|----------|-----|-------|
| Prometheus Docs | https://prometheus.io/docs | Metrics collection + PromQL |
| OpenTelemetry | https://opentelemetry.io/docs/languages/go | OTel Go SDK |
| Grafana Docs | https://grafana.com/docs/grafana | Dashboard creation |
| USE Method | https://www.brendangregg.com/usemethod.html | Utilization, Saturation, Errors |
| RED Method | https://grafana.com/blog/2018/08/02/the-red-method-how-to-instrument-your-services | Rate, Errors, Duration |
| Brendan Gregg Blog | https://www.brendangregg.com/blog | Performance engineering |
| Go Execution Tracer | https://go.dev/doc/diagnostics | go tool trace guide |
| FlameGraph | https://github.com/brendangregg/FlameGraph | Flame graph scripts |
| Vegeta (load test) | https://github.com/tsenart/vegeta | Go HTTP load tester |

---

## 6. Security

### Foundations
| Resource | URL | Notes |
|----------|-----|-------|
| OWASP Top 10 | https://owasp.org/www-project-top-ten | Top 10 web risks |
| OWASP Cheat Sheet Series | https://cheatsheetseries.owasp.org | Specific defense patterns |
| NIST Cybersecurity Framework | https://www.nist.gov/cyberframework | CSF 2.0 |
| CWE (MITRE) | https://cwe.mitre.org | Software weakness enumeration |
| CVE Database | https://cve.mitre.org | Vulnerability database |

### Go Security
| Resource | URL | Notes |
|----------|-----|-------|
| Go Security Policy | https://go.dev/security | Go CVE policy |
| gosec | https://github.com/securego/gosec | Go security linter |
| Nancy (Sonatype) | https://github.com/sonatype-oss/nancy | Go dependency audit |
| govulncheck | https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck | Official Go vuln scanner |
| Go crypto package | https://pkg.go.dev/crypto | Stdlib crypto reference |

### Supply Chain
| Resource | URL | Notes |
|----------|-----|-------|
| SLSA Framework | https://slsa.dev | Supply chain security levels |
| Sigstore / Cosign | https://docs.sigstore.dev | Artifact signing |
| SBOM Guide | https://www.ntia.gov/sbom | Software bill of materials |

### Threat Modeling
| Resource | URL | Notes |
|----------|-----|-------|
| STRIDE (Microsoft) | https://learn.microsoft.com/en-us/azure/security/develop/threat-modeling-tool-threats | STRIDE methodology |
| OWASP Threat Dragon | https://owasp.org/www-project-threat-dragon | Open source threat model tool |
| Threat Modeling Manifesto | https://www.threatmodelingmanifesto.org | Community principles |

### Go HTTP Security Hardening
| Resource | URL | Notes |
|----------|-----|-------|
| ResponseWriter concurrency | https://pkg.go.dev/net/http#ResponseWriter | "Callers should not mutate or reuse the request until the ServeHTTP method has returned" — not concurrent-safe |
| io.LimitReader | https://pkg.go.dev/io#LimitReader | Cap external response body reads to prevent OOM/DoS |
| OWASP WS Security Cheat Sheet | https://cheatsheetseries.owasp.org/cheatsheets/WebSockets_Cheat_Sheet.html | WS origin validation, message limits, auth |
| gorilla/websocket SetReadLimit | https://pkg.go.dev/github.com/gorilla/websocket#Conn.SetReadLimit | Default 0=unlimited; always set after upgrade |
| Cloudflare IP Ranges | https://www.cloudflare.com/ips | Authoritative IPv4/IPv6 lists for trusted proxy CIDR allowlist |
| net.IP.IsPrivate() | https://pkg.go.dev/net#IP.IsPrivate | SSRF guard — blocks RFC 1918, RFC 4193, loopback, link-local |
| OWASP SSRF Prevention Cheat Sheet | https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html | Private IP blocking, allowlist design |
| OWASP Error Handling Cheat Sheet | https://cheatsheetseries.owasp.org/cheatsheets/Error_Handling_Cheat_Sheet.html | Don't expose internals in error responses |
| OWASP REST Security Cheat Sheet | https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html | API key auth, error response hygiene, input validation |

---

## 6b. Offensive Security & Penetration Testing

### Methodology
| Resource | URL | Notes |
|----------|-----|-------|
| PTES (Pentest Execution Standard) | http://www.pentest-standard.org | Recon → scan → exploit → post-exploit → report framework |
| OSSTMM (Open Source Security Testing Methodology) | https://www.isecom.org/OSSTMM.3.pdf | Rigorous security testing methodology |
| OWASP Testing Guide (OTG) | https://owasp.org/www-project-web-security-testing-guide | Comprehensive web app pentest methodology |
| HackTricks | https://book.hacktricks.xyz | Techniques and payloads for pentest and CTF |
| PortSwigger Web Security Academy | https://portswigger.net/web-security | Free, hands-on web vulnerability labs (SQLi, XSS, SSRF, etc.) |
| PayloadsAllTheThings | https://github.com/swisskyrepo/PayloadsAllTheThings | Payload collections for web, API, and OS exploitation |

### Reconnaissance & OSINT
| Resource | URL | Notes |
|----------|-----|-------|
| Shodan | https://www.shodan.io | Internet-wide port/banner/service indexer — production use in vLog |
| Censys | https://search.censys.io | Certificate transparency + port scanning |
| theHarvester | https://github.com/laramies/theHarvester | Email/domain/IP OSINT aggregation |
| crt.sh | https://crt.sh | Certificate transparency log search |
| BGP.tools | https://bgp.tools | ASN/BGP routing intel |
| ip-api.com | https://ip-api.com/docs | Free bulk IP geo/org/ASN (45 req/min) — production use in vLog OSINTStream |
| ipinfo.io | https://ipinfo.io/developers | IP geo/ASN/hosting API with free tier |
| AbuseIPDB | https://www.abuseipdb.com/api | IP abuse/reputation database — production use in vLog EnrichStream |

### Scanning & Enumeration
| Resource | URL | Notes |
|----------|-----|-------|
| nmap reference | https://nmap.org/book/man.html | OS/service fingerprinting, NSE scripts |
| masscan | https://github.com/robertdavidgraham/masscan | Async TCP port scanner (millions of IPs/sec) |
| nuclei | https://github.com/projectdiscovery/nuclei | Template-based vulnerability scanner; good for API/web |
| ffuf | https://github.com/ffuf/ffuf | Web fuzzer — directory/parameter/vhost discovery |
| gobuster | https://github.com/OJ/gobuster | Directory/DNS/vhost brute-force |

### Web Exploitation & Proxies
| Resource | URL | Notes |
|----------|-----|-------|
| Burp Suite | https://portswigger.net/burp | Web proxy + active scanner (Community/Pro) |
| OWASP ZAP | https://www.zaproxy.org | Open-source web app scanner |
| SQLMap | https://sqlmap.org | Automated SQL injection and takeover |
| XSS Hunter | https://xsshunter.trufflesecurity.com | Blind XSS callback platform |

### Exploitation Frameworks & References
| Resource | URL | Notes |
|----------|-----|-------|
| Metasploit Framework | https://docs.metasploit.com | Industry-standard exploit and post-exploit framework |
| ExploitDB | https://www.exploit-db.com | Public exploit archive; GHDB for Google dorks |
| GTFOBins | https://gtfobins.github.io | Unix binary privilege escalation and bypass |
| LOLBAS | https://lolbas-project.github.io | Windows living-off-the-land binaries |
| CyberChef | https://gchq.github.io/CyberChef | Encode/decode/transform/analyze data |

### Responsible Disclosure & Whitehack Ethics
| Resource | URL | Notes |
|----------|-----|-------|
| CERT/CC Guide to CVD | https://certcc.github.io/CERT-Guide-to-CVD | Coordinated vulnerability disclosure best practices |
| security.txt (RFC 9116) | https://securitytxt.org | Machine-readable security contact standard |
| HackerOne Hacktivity | https://hackerone.com/hacktivity | Bug bounty disclosures (learning by example) |
| Bugcrowd VRT | https://bugcrowd.com/vulnerability-rating-taxonomy | Vulnerability rating taxonomy and severity guidance |
| CVSS v3.1 Calculator | https://www.first.org/cvss/calculator/3.1 | CVSS scoring reference |
| Google Project Zero | https://project-zero.google | High-quality 90-day disclosure examples |

---

## 7. Architecture & Distributed Systems

| Resource | URL | Notes |
|----------|-----|-------|
| Designing Data-Intensive Applications | https://dataintensive.net | Kleppmann — essential reference |
| System Design Primer | https://github.com/donnemartin/system-design-primer | Curated system design |
| AWS Architecture Center | https://aws.amazon.com/architecture | Patterns & reference architectures |
| Martin Fowler Bliki | https://martinfowler.com | Architecture patterns |
| CAP Theorem (Brewer) | https://www.infoq.com/articles/cap-twelve-years-later-how-the-rules-have-changed | CAP theorem nuances |
| Reverse Proxy Patterns | https://www.nginx.com/blog/reverse-proxy-using-nginx | Proxy design patterns |
| Rate Limiting Algorithms | https://blog.cloudflare.com/counting-things-a-lot-of-different-things | Cloudflare token bucket |

---

## 8. Testing

| Resource | URL | Notes |
|----------|-----|-------|
| Google Testing Blog | https://testing.googleblog.com | Test patterns & practices |
| go testing package | https://pkg.go.dev/testing | Official docs |
| Table-driven tests (Go blog) | https://dave.cheney.net/2019/05/07/prefer-table-driven-tests | Idiomatic test patterns |
| go-fuzz tutorial | https://github.com/dvyukov/go-fuzz#usage | Fuzz testing in Go |
| testify | https://pkg.go.dev/github.com/stretchr/testify | Assertions + mocks |
| httptest | https://pkg.go.dev/net/http/httptest | HTTP test utilities |
| Property-based testing | https://hypothesis.works | Hypothesis testing principles |

---

## 9. Scientific & Research Methodology

| Resource | URL | Notes |
|----------|-----|-------|
| The Pragmatic Programmer | https://pragprog.com/titles/tpp20 | Engineering craft |
| Clean Code (Martin) | https://www.oreilly.com/library/view/clean-code-a/9780136083238 | Code quality principles |
| Staff Engineer (Larson) | https://staffeng.com/book | Technical leadership |
| 5 Whys | https://www.mindtools.com/a3mi00v/5-whys | Root cause analysis |
| Fishbone / Ishikawa | https://asq.org/quality-resources/fishbone | Cause-effect diagrams |
| Decision Trees & MCDA | https://www.sciencedirect.com/science/article/pii/S0377221706009076 | Multi-criteria decision analysis |
| Monte Carlo for Estimation | https://erikbern.com/2016/12/15/use-monte-carlo-simulations-for-your-project-estimates | Engineering estimation |
| arxiv (preprints) | https://arxiv.org/list/cs.DC/recent | Distributed computing papers |
| Papers With Code | https://paperswithcode.com | ML papers + code |

---

## 10. vProx-Specific References

| Resource | URL | Notes |
|----------|-----|-------|
| gorilla/websocket | https://pkg.go.dev/github.com/gorilla/websocket | Primary WS library |
| golang.org/x/time/rate | https://pkg.go.dev/golang.org/x/time/rate | Token bucket implementation |
| IP2Location Go | https://github.com/ip2location/ip2location-go | MMDB reader alternative |
| MaxMind geoip2-golang | https://github.com/oschwald/geoip2-golang | Current MMDB reader |
| go-toml/v2 | https://github.com/pelletier/go-toml | TOML config library |
| systemd units reference | https://www.freedesktop.org/software/systemd/man/systemd.unit.html | Service template reference |
| GitHub Actions Docs | https://docs.github.com/en/actions | Workflow syntax reference |
| GitHub branch protection | https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches | Branch protection API |

---

## 10b. Web Server Engineering

### HTTP / TLS / Middleware
| Resource | URL | Notes |
|----------|-----|-------|
| net/http package | https://pkg.go.dev/net/http | ServeMux, Server, Handler, ResponseWriter |
| net/http/httputil | https://pkg.go.dev/net/http/httputil | ReverseProxy, DumpRequest |
| crypto/tls | https://pkg.go.dev/crypto/tls | TLS config, SNI, GetCertificate |
| compress/gzip | https://pkg.go.dev/compress/gzip | Response compression |
| ResponseWriter Unwrap | https://pkg.go.dev/net/http#NewResponseController | Go 1.20+ Unwrap protocol |
| HTTP/2 in Go | https://pkg.go.dev/golang.org/x/net/http2 | h2c transport, HTTP/2 server |
| CORS spec | https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS | CORS protocol reference |
| Let's Encrypt | https://letsencrypt.org/docs | ACME protocol, cert automation |
| certmagic | https://pkg.go.dev/github.com/caddyserver/certmagic | Automatic TLS cert management |

### Reverse Proxy Patterns
| Resource | URL | Notes |
|----------|-----|-------|
| Caddy server | https://caddyserver.com/docs | Modern Go reverse proxy reference |
| Traefik | https://doc.traefik.io/traefik | Dynamic reverse proxy patterns |
| nginx proxy docs | https://nginx.org/en/docs/http/ngx_http_proxy_module.html | Proxy directive reference |
| WebSocket proxy | https://pkg.go.dev/github.com/gorilla/websocket#hdr-Handler | WS upgrade handling |

### Config Architecture
| Resource | URL | Notes |
|----------|-----|-------|
| TOML spec | https://toml.io/en | TOML language specification |
| go-toml v2 | https://github.com/pelletier/go-toml | Marshaling/unmarshaling |
| Apache mod_proxy | https://httpd.apache.org/docs/2.4/mod/mod_proxy.html | Apache proxy directives (import source) |
| Apache VirtualHost | https://httpd.apache.org/docs/2.4/vhosts | VirtualHost configuration reference |

---

## Quick Domain Lookup

```
resources go          → Sections 1
resources cosmos      → Section 2
resources stats       → Section 3.1
resources ml          → Section 3.2, 4
resources anomaly     → Section 3.2 (Anomaly Detection)
resources obs         → Section 5
resources security    → Section 6 (defensive + Go HTTP hardening) + Section 6b (offensive/pentest/whitehack)
resources arch        → Section 7
resources testing     → Section 8
resources research    → Section 9
resources vprox       → Section 10
resources webserver   → Section 10b
resources webservice  → Section 15b
resources ci          → Section 11
resources docs        → Section 12
resources ai          → Section 13
resources release     → Section 14
resources ebpf        → Section 14
resources webgui      → Section 15 (CSS/dashboard/htmx/SSE)
resources auth        → Section 16 (session auth, bcrypt, cookies)
resources css         → Section 15 (CSS custom props, glass morphism, background-size)
```

---

## 11. GitHub Actions & CI/CD

| Resource | URL | Notes |
|---|---|---|
| GitHub Actions Docs | https://docs.github.com/en/actions | Complete reference |
| Workflow syntax | https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions | YAML schema |
| Reusable workflows | https://docs.github.com/en/actions/sharing-automations/reusing-workflows | DRY CI patterns |
| GitHub Script action | https://github.com/actions/github-script | JS scripting in workflows |
| OIDC token federation | https://docs.github.com/en/actions/security-for-github-actions/security-hardening-your-deployments/about-security-hardening-with-openid-connect | Secretless auth |
| Dependabot config | https://docs.github.com/en/code-security/dependabot/dependabot-version-updates/configuration-options-for-the-dependabot.yml-file | Version update options |
| Dependency Review | https://docs.github.com/en/code-security/supply-chain-security/understanding-your-software-supply-chain/about-dependency-review | Supply chain review |
| Branch protection | https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches | Protection rule reference |

---

## 12. Technical Documentation

| Resource | URL | Notes |
|---|---|---|
| Keep a Changelog | https://keepachangelog.com/en/1.1.0 | CHANGELOG format standard |
| Semantic Versioning | https://semver.org | Version numbering spec |
| Write the Docs | https://www.writethedocs.org/guide | Documentation engineering guides |
| Divio Documentation System | https://documentation.divio.com | Tutorial/how-to/ref/explanation quadrant |
| Google Developer Documentation Style | https://developers.google.com/style | Clear technical writing guide |
| Markdown spec (CommonMark) | https://spec.commonmark.org | Authoritative Markdown spec |
| Git .gitignore patterns | https://git-scm.com/docs/gitignore | Pattern matching reference |
| Git archive / export | https://git-scm.com/docs/git-archive | export-ignore and tarball generation |

---

## 13. AI Agent Orchestration & LLM Tooling

| Resource | URL | Notes |
|---|---|---|
| GitHub Copilot CLI Docs | https://docs.github.com/en/copilot/using-github-copilot/using-github-copilot-in-the-command-line | Copilot CLI usage |
| GitHub Copilot Agents | https://docs.github.com/en/copilot/customizing-copilot/using-copilot-agents | Custom agent files (.agent.md) |
| OpenAI API Reference | https://platform.openai.com/docs/api-reference | LLM API reference |
| HuggingFace Transformers | https://huggingface.co/docs/transformers | Model hub + pipelines |
| LangChain | https://python.langchain.com/docs | LLM orchestration framework |
| Prompt Engineering Guide | https://www.promptingguide.ai | Comprehensive prompting techniques |
| Chain-of-Thought Prompting | https://arxiv.org/abs/2201.11903 | Wei et al., CoT paper |
| ReAct (Reason + Act) | https://arxiv.org/abs/2210.03629 | Agent reasoning with tools |

---

## 14. Release Automation & eBPF

### Release Automation
| Resource | URL | Notes |
|---|---|---|
| GoReleaser | https://goreleaser.com/docs | Go release automation |
| GoReleaser GitHub Action | https://github.com/goreleaser/goreleaser-action | CI integration |
| Cosign (artifact signing) | https://docs.sigstore.dev/cosign/signing/overview | Keyless artifact signing |
| SBOM generation (syft) | https://github.com/anchore/syft | Software bill of materials |
| GitHub Releases API | https://docs.github.com/en/rest/releases | REST API for release management |

### eBPF / Kernel Tracing (Future)
| Resource | URL | Notes |
|---|---|---|
| cilium/ebpf (Go) | https://pkg.go.dev/github.com/cilium/ebpf | Go eBPF library |
| BCC (BPF Compiler Collection) | https://github.com/iovisor/bcc | Python/C eBPF tools |
| bpftrace | https://github.com/bpftrace/bpftrace | High-level eBPF scripting |
| eBPF.io | https://ebpf.io/what-is-ebpf | eBPF overview and use cases |
| Linux Observability with BPF | https://www.oreilly.com/library/view/linux-observability-with/9781492050193 | O'Reilly eBPF book |

---

## 15. Web GUI Engineering (Go)

### Core Stack (vProx recommended)
| Resource | URL | Notes |
|---|---|---|
| html/template | https://pkg.go.dev/html/template | Go standard template engine, context-aware escaping |
| go:embed directive | https://pkg.go.dev/embed | Embed static assets in Go binary |
| htmx | https://htmx.org | HTML-driven AJAX, SSE, WebSocket with zero JS framework |
| htmx reference | https://htmx.org/reference | Complete attribute/event/header reference |
| htmx examples | https://htmx.org/examples | Active search, lazy loading, infinite scroll, SSE |

### Templating & Alternatives
| Resource | URL | Notes |
|---|---|---|
| templ | https://templ.guide | Type-safe Go HTML templating (compile-time checked) |
| go-htmx (integration) | https://github.com/donseba/go-htmx | Go middleware for htmx request/response handling |
| Go + htmx CRUD tutorial | https://dev.to/coderonfleek/htmx-go-build-a-crud-app-with-golang-and-htmx-1le2 | Step-by-step walkthrough |
| Go + htmx app guide | https://dev.to/calvinmclean/how-to-build-a-web-application-with-htmx-and-go-3183 | Pattern reference |

### CSS & Dashboard UI
| Resource | URL | Notes |
|---|---|---|
| Pico CSS | https://picocss.com | Classless CSS framework, minimal footprint, dark mode |
| Pico CSS dark mode | https://picocss.com/docs/color-schemes | `data-theme="dark"`, `--pico-*` overrides |
| CSS custom properties | https://developer.mozilla.org/en-US/docs/Web/CSS/Using_CSS_custom_properties | CSS variable design token system |
| Simple.css | https://simplecss.org | Classless, lightweight, semantic HTML styling |
| AdminLTE | https://adminlte.io | Dashboard template (heavier, if needed) |
| Chart.js | https://www.chartjs.org | Lightweight charts for dashboards |
| backdrop-filter (MDN) | https://developer.mozilla.org/en-US/docs/Web/CSS/backdrop-filter | Glass morphism blur effect |
| CSS glass morphism guide | https://css.glass | Generator + browser compat notes for glass morphism |
| CSS background-size (MDN) | https://developer.mozilla.org/en-US/docs/Web/CSS/background-size | `cover`/`contain`/`100% 100%` reference |

### Architecture Patterns
| Resource | URL | Notes |
|---|---|---|
| SSE in Go | https://pkg.go.dev/net/http#Flusher | Flusher interface for streaming responses |
| go-app (WASM alternative) | https://go-app.dev | All-Go browser apps via WebAssembly (future exploration) |
| Wails | https://wails.io | Desktop hybrid (Go+Web) — evaluated, rejected for headless servers |
| GoAdmin | https://github.com/GoAdminGroup/go-admin | Full admin framework (evaluated, too heavy for vProx) |

---

## 15b. Web Service Architecture & Design

### Embedded Server Patterns
| Resource | URL | Notes |
|----------|-----|-------|
| Go HTTP server tutorial | https://go.dev/doc/articles/wiki | Official Go web server walkthrough |
| Graceful shutdown | https://pkg.go.dev/net/http#Server.Shutdown | Shutdown with context deadline |
| Go middleware patterns | https://justinas.org/writing-http-middleware-in-go | Composable handler wrapping |
| Alice (middleware chain) | https://github.com/justinas/alice | Lightweight middleware chaining |

### TLS & Certificate Management
| Resource | URL | Notes |
|----------|-----|-------|
| certmagic | https://pkg.go.dev/github.com/caddyserver/certmagic | Automatic ACME/TLS certs in Go |
| Let's Encrypt ACME spec | https://letsencrypt.org/docs/challenge-types | Challenge types: HTTP-01, DNS-01, TLS-ALPN-01 |
| autocert (stdlib) | https://pkg.go.dev/golang.org/x/crypto/acme/autocert | Official Go ACME client |
| SSL Labs best practices | https://github.com/ssllabs/research/wiki/SSL-and-TLS-Deployment-Best-Practices | TLS configuration reference |

### Apache/nginx Migration
| Resource | URL | Notes |
|----------|-----|-------|
| Apache → Go migration | https://httpd.apache.org/docs/2.4/mod/mod_proxy.html | mod_proxy directive mapping |
| nginx → Go reverse proxy | https://nginx.org/en/docs/http/ngx_http_proxy_module.html | proxy_pass equivalents |
| Apache VirtualHost | https://httpd.apache.org/docs/2.4/vhosts | VirtualHost → per-vhost TOML mapping |
| nginx server blocks | https://nginx.org/en/docs/http/server_names.html | server_name → host routing |

### Load Balancing & Health
| Resource | URL | Notes |
|----------|-----|-------|
| Go load balancer patterns | https://pkg.go.dev/net/http/httputil#ReverseProxy | Transport customization for LB |
| Circuit breaker (Go) | https://github.com/sony/gobreaker | Circuit breaker library |
| Health check patterns | https://microservices.io/patterns/observability/health-check-api.html | Readiness/liveness patterns |

### Security Headers
| Resource | URL | Notes |
|----------|-----|-------|
| OWASP Secure Headers | https://owasp.org/www-project-secure-headers | Header recommendations |
| MDN security headers | https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers#security | Security header reference |
| Helmet.js (reference) | https://helmetjs.github.io | Header defaults (inspiration for Go impl) |

---

## 16. Session Auth & Web Security (vLog)

| Resource | URL | Notes |
|---|---|---|
| golang.org/x/crypto/bcrypt | https://pkg.go.dev/golang.org/x/crypto/bcrypt | Bcrypt password hashing — production use in vLog auth |
| crypto/rand | https://pkg.go.dev/crypto/rand | Cryptographically secure random bytes for session tokens |
| net/http Cookie | https://pkg.go.dev/net/http#Cookie | HttpOnly, SameSite, Secure flags reference |
| OWASP Session Management | https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html | Token size, TTL, cookie flags, invalidation |
| htpasswd bcrypt | https://httpd.apache.org/docs/2.4/misc/password_encryptions.html | Apache htpasswd bcrypt format (compatible with Go bcrypt) |
| golang.org/x/crypto/acme/autocert | https://pkg.go.dev/golang.org/x/crypto/acme/autocert | ACME TLS cert management |

---

## 17. Infrastructure Deployment & SSH (vProx push module)

| Resource | URL | Notes |
|----------|-----|-------|
| golang.org/x/crypto/ssh | https://pkg.go.dev/golang.org/x/crypto/ssh | SSH client — production in `internal/push/ssh/` |
| SSH authorized_keys format | https://man.openbsd.org/sshd.8#AUTHORIZED_KEYS_FILE_FORMAT | Key options, from= restriction, command= restriction |
| CometBFT `/status` sync_info | https://docs.cometbft.com/v0.38/rpc/#/Info/status | `catching_up` bool; `latest_block_height`; used for node health routing |
| Cosmos upgrade API | https://docs.cosmos.network/api#tag/Upgrade | `/current_plan`, `/applied_plan`, `/module_versions` — upgrade automation |
| gobreaker (circuit breaker) | https://github.com/sony/gobreaker | Circuit breaker for `broadcast_tx_commit` fallback pattern |

---

*Last updated: 2026-03-03 (rev14: §2b Cosmos SDK Hidden Gems NEW — WS limits, upgrade detection, IBC DoS patterns, ABCI cost split, mempool health; §17 Infrastructure Deployment NEW — SSH, sync detection, upgrade API, circuit breaker; Cosmos SDK depth 3.5→4 across CometBFT+IBC; Quick Domain Lookup updated)*
