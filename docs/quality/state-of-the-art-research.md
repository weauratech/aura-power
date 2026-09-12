# Aura Power: referência técnica para uma implementação open source de alto nível

## Escopo e critério de avaliação

Este relatório converte os problemas observados no Aura Power em práticas verificáveis, testes de aceitação e uma sequência incremental de mudanças. A análise usa o repositório no commit `4727bc5` e os registros de revisão produzidos pela campanha de qualidade. As recomendações não pressupõem que um comportamento observado em leitura de código já tenha sido confirmado em um cluster: quando falta reprodução, o relatório o trata como hipótese e especifica a validação necessária.

O padrão pretendido não é uma lista de tecnologias. Para o Aura Power, uma implementação de alto nível precisa demonstrar sete propriedades:

1. **Exatidão de seleção:** apenas os workloads identificados pelo contrato de escopo podem ser alterados.
2. **Recuperação comprovável:** estado anterior, identidade do objeto e progresso da restauração sobrevivem a reinícios, conflitos e repetição do reconcile.
3. **Semântica única:** CRD, API HTTP, CLI, frontend, auditoria e documentação descrevem o mesmo alvo, a mesma decisão e o mesmo resultado.
4. **Convivência declarativa:** a autoridade de cada campo é explícita quando Argo CD, HPA, KEDA ou outra automação também controla o recurso.
5. **Segurança por padrão:** falhas de autenticação, autorização, validação, persistência e dependências não se transformam em sucesso ou acesso implícito.
6. **Operação observável:** pedido, decisão, execução, efeito e recuperação têm identidade e estado correlacionáveis.
7. **Distribuição confiável:** código, chart, imagens e binários são reproduzíveis, rastreáveis até a origem e verificáveis pelo consumidor.

Essas propriedades formam critérios de aceite. Cobertura de linhas, scanner de acessibilidade, Scorecard ou uma classificação SLSA são sinais parciais; nenhum deles substitui uma jornada com efeito e recuperação observados.

## Evidência de referência

O Kubernetes define controllers como loops que observam estado e trabalham para aproximar o estado atual do desejado. O Kubebuilder reforça que o reconcile precisa ser idempotente para responder corretamente a eventos esperados e inesperados, inicialização e upgrade.[^1] Isso torna repetição e concorrência partes do contrato, não casos excepcionais.

O `envtest` inicia etcd e API server, sem kubelet, controller-manager ou os demais controllers nativos. Ele é adequado para schema, admission, status, conflitos e watches, mas não comprova criação de Pods, prontidão, execução de Jobs ou garbage collection real.[^2] A camada Kind e a aceitação controlada em EKS são necessárias para essas propriedades.

Objetos Kubernetes têm identidade por `apiVersion`, `kind`, namespace, nome e UID. Owner references também incluem UID para distinguir uma instância recriada com o mesmo nome.[^3] Para um produto que desliga e restaura workloads, omitir kind ou UID da identidade persistida abre colisões entre tipos e restauração sobre uma nova encarnação do objeto.

Custom resources devem usar schema estrutural, status separado e evolução de versão planejada. Kubernetes permite servir versões em paralelo e migrar objetos gradualmente; mudanças semânticas que exigem conversão precisam de webhook, e uma versão antiga não deve ser removida antes da migração do storage.[^4] Isso oferece um caminho seguro para corrigir contratos de escopo e identidade sem editar objetos existentes de forma destrutiva.

Para CronJobs, `spec.suspend=true` impede execuções futuras, não afeta Jobs já iniciados e conta horários suspensos como execuções perdidas. Ao reativar sem `startingDeadlineSeconds`, Jobs perdidos podem ser iniciados imediatamente.[^5] Portanto, “CronJob desligado” precisa ser uma definição explícita; suspensão do agendamento não equivale a interrupção de trabalho em execução.

## Matriz de práticas aplicáveis

As versões abaixo identificam o contrato consultado: Kubernetes 1.31 é a linha das dependências Go atuais; a validação real também deve cobrir as versões declaradas pelo projeto e a versão do EKS. Argo CD refere-se à documentação estável consultada; ASVS refere-se à versão 5.0.0; SLSA refere-se à especificação 1.2. Para ferramentas com publicação contínua, o link é a fonte de verdade e a versão efetivamente usada deve permanecer fixada na CI.

### Reconciliação, estado e identidade

| Problema observado | Prática de referência e fonte | Versão ou contrato | Aplicabilidade ao Aura Power | Alternativa mais simples | Validação proposta | Custo |
|---|---|---|---|---|---|---|
| O workload é mutado antes de o snapshot ser confirmado no status; uma falha entre as duas operações pode perder o estado original | Reconcile idempotente, orientado ao estado, com status conditions e persistência antes do efeito[^1] | Kubebuilder/controller-runtime, contrato contínuo | Introduzir uma operação persistida com UID, geração, snapshot, fase e condição antes de mutar; confirmar efeito numa etapa posterior | Persistir snapshot no `PowerTarget.status` e abortar a mutação se a escrita falhar | Teste de queda em cada ponto: leitura, gravação do snapshot, patch do workload, confirmação; reiniciar controller e exigir restauração original | Alto |
| Atualizações de status podem sofrer `409` e o erro não pode virar sucesso | Usar optimistic concurrency, repetir leitura/cálculo e reportar condição observável; o API server oferece `resourceVersion` e watches consistentes[^6] | Kubernetes API | Retornar erro de reconcile para backoff da workqueue; não registrar sucesso antes de persistir estado conclusivo | `RetryOnConflict` restrito à atualização de status, recomputando a partir da leitura atual | Cliente com conflito injetado; confirmar ausência de evento de sucesso e convergência após a repetição | Médio |
| Uma segunda reconciliação pode capturar zero após a primeira escala e substituir o snapshot original | Tornar cada transição monotônica e idempotente[^1] | Kubebuilder good practices | Operação em `Pending -> SnapshotCaptured -> Applying -> Applied -> Restoring -> Restored/Failed`; snapshot imutável durante a operação | Se snapshot disponível, nunca chamar `PowerDown` novamente; apenas confirmar o efeito | Duas reconciliações simultâneas e discovery atrasado; snapshot deve conservar a réplica inicial | Médio |
| Restauração de CronJob aparenta definir `suspend=false`, ignorando o valor capturado | Restaurar exatamente o estado observado; considerar semântica de Jobs já iniciados e execuções perdidas[^5] | batch/v1 CronJob | Restaurar o booleano do snapshot e expor política para Jobs ativos/missed runs | Restaurar somente `snapshot.suspended` e documentar que Jobs ativos não são interrompidos | CronJob originalmente suspenso e ativo; Job já iniciado; janela sem/com `startingDeadlineSeconds` | Baixo para correção, médio para contrato completo |
| Rotas e chaves usam namespace/nome, embora kinds homônimos sejam válidos e objetos possam ser recriados | Usar identidade canônica com group, kind, namespace, name e UID; UID distingue encarnações[^3] | ObjectMeta/owner references | Definir `TargetIdentity` comum e exigir UID antes de qualquer mutação/restauração | Adicionar kind às rotas imediatamente e bloquear restauração quando UID divergir | Deployment, StatefulSet e CronJob homônimos; excluir/recriar entre power-down e restore | Alto devido à migração de contrato |
| Um único reconciliador carrega políticas e overrides globalmente | Controllers devem ter responsabilidade coesa; escopo precisa estar explícito[^1] | Kubebuilder good practices | Indexar políticas por control namespace/tenant e observar somente os namespaces admitidos | Aplicar `client.InNamespace` para recursos de controle e um allowlist de namespaces alvo | Duas instalações em namespaces distintos, políticas homônimas e alvos disjuntos; nenhuma interferência cruzada | Médio |
| Estado desejado, observado, ação aceita e ação concluída se confundem | Usar `status.conditions` padronizadas, `observedGeneration` e razões estáveis[^1] | Kubernetes API conventions | Separar `DecisionReady`, `ActionInProgress`, `ActionApplied`, `RestoreVerified`, `Blocked` e `Degraded` | Acrescentar fase, operation ID e `observedGeneration` sem remover campos existentes | Alterar spec durante uma ação; UI/CLI não podem chamar a geração antiga de concluída | Médio |

### Escopo, API e contratos

| Problema observado | Prática de referência e fonte | Versão ou contrato | Aplicabilidade ao Aura Power | Alternativa mais simples | Validação proposta | Custo |
|---|---|---|---|---|---|---|
| NamespaceGroups existem no CRD, mas o fluxo de conversão pode perder grupos; escopo vazio pode selecionar tudo | CRDs com schema estrutural, validação no API server e admission para invariantes entre campos[^4] | apiextensions.k8s.io/v1 | Tornar o modo de seleção uma união discriminada (`all`, `namespaces`, `targets`, `selector`, `groups`) e proibir vazio implícito | Rejeitar escopo vazio e preservar `groups` na conversão atual | Golden tests CRD -> domínio -> preview -> execução; fuzzing de combinações e ausência | Médio |
| UI serializa listas separadas de namespaces e nomes, incapazes de preservar pares exatos | Um contrato deve representar diretamente a entidade pretendida; schemas públicos precisam ser inequívocos[^4] | CRD versionada + OpenAPI HTTP | Lista de referências completas, cada uma com group/kind/namespace/name; resolução retorna UID antes de executar | Usar strings `kind:namespace/name` como etapa compatível | Selecionar `alpha/api` e `beta/worker`; provar que `alpha/worker` e `beta/api` nunca são afetados | Alto |
| Namespaces de controle são fixados em `aura-system` em UI/API/CLI | Charts públicos devem permitir values sobre os aspectos configuráveis e documentá-los[^7] | Helm 3 best practices | Passar o namespace efetivo do release por configuração única ao server, controller e frontend | API usa namespace de execução do Pod e frontend nunca envia `metadata.namespace` | Instalar duas releases isoladas em Kind com namespaces não padrão; comparar objetos criados | Médio |
| Preview pode divergir da execução por conversões e leituras distintas | Um único motor de decisão puro, chamado por preview e reconcile, com versão de decisão e conjunto resolvido | Arquitetura recomendada baseada no loop Kubernetes[^1] | Preview devolve identidade/UID/generation dos alvos, regra vencedora, blocks e hash da entrada | Golden tests usando as mesmas fixtures em ambos os adaptadores | Teste metamórfico: mesma entrada e relógio produzem mesmo conjunto/decisão em domínio, API e controller | Médio |
| API, CLI e frontend têm tipos e tratamento de erros duplicados | Gerar clientes/tipos de um schema versionado e modelar estados de erro exaustivos | Kubernetes/OpenAPI e TypeScript strict[^4][^8] | OpenAPI como contrato HTTP; geração TypeScript e Go; erros RFC 9457 ou envelope documentado | Golden JSON/YAML e testes de compatibilidade por fixture | Payloads válidos e inválidos atravessam CRD, domínio, API, CLI e UI sem perda semântica | Médio |
| Mudanças públicas podem quebrar CRs, URLs e automações existentes | Servir versões CRD em paralelo, converter e migrar storage antes de remover a antiga[^4] | Kubernetes CRD versioning | Criar `v1alpha2` ou `v1beta1` para identidade/escopo quando a compatibilidade não puder ser aditiva | Campos novos opcionais em `v1alpha1`, com warning e telemetria de uso antigo | Round trip entre versões; backup/restore; upgrade e downgrade suportado | Alto |

### GitOps, autoscaling e instalação

| Problema observado | Prática de referência e fonte | Versão ou contrato | Aplicabilidade ao Aura Power | Alternativa mais simples | Validação proposta | Custo |
|---|---|---|---|---|---|---|
| Documentação sugere `ignoreDifferences`, mas isso sozinho altera comparação, não o apply da sincronização | Usar `RespectIgnoreDifferences=true`; reconhecer que a opção só funciona para recurso já existente[^9] | Argo CD estável | Chart/documentação devem fornecer exemplo completo e a suíte deve testar criação inicial separadamente | Marcar a integração como experimental até a matriz passar | Auto-sync/self-heal com e sem ambas as opções; recurso existente e criação inicial; medir oscilação | Baixo para docs, médio para validação |
| Detecção de Argo CD por qualquer annotation pode não corresponder ao tracking real | Argo CD admite `annotation`, `annotation+label`, `label` e label customizada; a annotation carrega group/kind/ns/name[^10] | Argo CD Resource Tracking | Detectar os métodos suportados com configuração explícita, sem inferir ownership de qualquer prefixo | Opt-in obrigatório para todo alvo GitOps e warning informativo para sinais não conclusivos | Matriz dos três métodos, label customizada e duas instalações com installation ID | Médio |
| Argo CD self-heal pode disputar `/spec.replicas`; ApplicationSet tem autoridade própria sobre sync policy | Auto-sync reconcilia drift e self-heal tenta sincronizar novamente; alterações diretas em Applications geradas podem não ter efeito[^11] | Argo CD Automated Sync | Declarar autoridade por campo e exigir configuração na fonte ApplicationSet quando aplicável | Bloquear por padrão recursos com tracking detectado sem opt-in explícito | Teste temporal: Aura Power mantém off sem loop; mudança legítima de imagem no Git continua chegando | Médio |
| HPA/KEDA/Flux aparecem como integrações, mas presença de enum ou docs não prova convivência | Compatibilidade é um contrato por controlador e campo, demonstrado por cluster completo | Kubernetes control-loop model[^1] | Adaptadores/capabilities separados e estados `Supported`, `DetectedBlocked`, `Experimental` | Bloquear automações concorrentes desconhecidas e documentar o motivo | HPA/KEDA alterando réplicas, Flux reconciliando workload; verificar ausência de disputa e recuperação | Alto |
| Chart precisa provar que values chegam à configuração efetiva | Helm recomenda values documentados; chart tests validam componentes instalados[^7][^12] | Helm 3 | `values.schema.json`, `helm lint/template`, chart test de saúde/auth/config e E2E de install/upgrade/uninstall | Snapshots de manifests para combinações críticas | Instalação não padrão, persistence on/off, NetworkPolicy, ServiceMonitor e upgrade com PVC | Médio |

### API, autenticação, frontend e experiência

| Problema observado | Prática de referência e fonte | Versão ou contrato | Aplicabilidade ao Aura Power | Alternativa mais simples | Validação proposta | Custo |
|---|---|---|---|---|---|---|
| Falha de `/auth/me` é interpretada pelo frontend como autenticação desabilitada | Segurança deve falhar fechada; ASVS separa autenticação, sessão, autorização e segurança de API em requisitos verificáveis[^13] | OWASP ASVS 5.0.0 | Estado explícito `unauthenticated`, `authenticated`, `auth-disabled-by-server`, `unavailable`; somente configuração assinada habilita modo sem auth | Qualquer erro de rede ou 5xx leva a uma tela indisponível sem dados operacionais | MSW, proxy real e browser: 401, 403, 500, timeout, HTML, conexão recusada | Baixo |
| Access e refresh tokens podem compartilhar semântica e validação | Tokens devem ter finalidade, audiência, expiração, rotação e revogação verificadas; aplicar requisitos ASVS por identificador versionado[^13] | OWASP ASVS 5.0.0 | Claims `typ`, `aud`, `iss`, `jti`; refresh rotativo armazenado por hash; rebaixamento/remove invalida sessões | Chaves e TTLs distintos, endpoint de refresh rejeita token de acesso | Matriz de troca de tokens, replay, expiração, clock skew, logout, usuário removido/rebaixado | Médio |
| Aprovações são expostas, mas a aplicação da mudança e a página podem estar incompletas | Um fluxo deve ser atômico, autorizado no servidor e auditável; UI não é fronteira de autorização[^13] | OWASP ASVS 5.0.0 | State machine `Pending/Approved/Rejected/Applied/Failed/Expired` com compare-and-set e autoria | Ocultar a funcionalidade pública até concluir API e UI | Dois approvers concorrentes, self-approval, role downgrade, expiração, aplicação falha e retry seguro | Alto |
| 504 de notificações vira coleção vazia; outros erros usam regras diferentes | Modelar indisponibilidade, vazio, forbidden, validation e conflict separadamente; TypeScript `strict` e índices verificados reduzem estados impossíveis[^8] | TypeScript 5.x | Cliente único tipado, Problem Details, React Query com política de retry por classe | Helper central de fetch e componentes padrão de erro/vazio | Contract tests HTTP e componentes; falha parcial nunca oferece criação como se a coleção fosse vazia | Médio |
| Efeitos, submissões e múltiplas abas podem duplicar operações | React recomenda componentes puros e StrictMode executa ciclos extras de setup/cleanup para revelar efeitos frágeis[^14] | React 18 | Idempotency key nas mutações, abort/cancel no cliente e invalidation determinística | Desabilitar submit durante request e garantir cleanup dos effects | StrictMode, duplo clique, voltar/avançar, duas abas e resposta fora de ordem | Médio |
| Testes Playwright usavam defaults de produção, credenciais fixas, passos condicionais e asserções tautológicas | Testar comportamento visível, isolar estado, usar locators por papel/nome e traces; retries classificam flakiness e não devem mascarar falha[^15] | Playwright atual fixado no lockfile | Fixtures únicas, URL/credenciais obrigatórias, mutação por flag, cleanup verificado, zero assertions condicionais | Chromium read-only por PR; matriz completa em execução ampliada | Rodar suite em servidor real, falhar se nenhuma asserção de efeito ocorreu e revisar trace de toda falha | Médio |
| Scanner automático pode ser confundido com conformidade de acessibilidade | Playwright recomenda axe junto de avaliação manual e testes inclusivos; automação não detecta todas as violações[^16] | WCAG 2.2 AA como alvo da campanha | Axe por página/estado, teclado, foco, zoom, contraste e avaliação assistiva manual documentada | Axe + teclado + zoom como gate inicial | Chromium/Firefox/WebKit, mobile/desktop, light/dark, erros/modais/loading e leitor de tela amostral | Médio |

### Código Go, confiabilidade e segurança de entrega

| Problema observado | Prática de referência e fonte | Versão ou contrato | Aplicabilidade ao Aura Power | Alternativa mais simples | Validação proposta | Custo |
|---|---|---|---|---|---|---|
| Testes simulados não exercitam schema, admission, watches, conflitos ou controllers nativos | Pirâmide com unit/property/fuzz, `envtest` e cluster completo, respeitando os limites de cada nível[^2] | controller-runtime compatível com k8s.io 0.31 | Unitários puros; envtest para API; Kind para workload/Job/Helm/GitOps; EKS para infraestrutura real | Começar por envtest de CRDs/webhooks e smoke Kind | Cada requisito da matriz marca ambiente e dependências reais/simuladas | Médio |
| Parsers, escopos, calendários, conversões e handlers têm grande espaço de entrada | Go inclui fuzzing nativo para descobrir entradas que causam panics, violações e erros inesperados; race detector detecta acessos concorrentes reais[^17][^18] | Go 1.25 declarado | Seeds de regressão para horários/DST, YAML/JSON, nomes, seletores e conversões; `-race` em PR | Property tests determinísticos nos invariantes mais críticos | Corpus versionado; falha reproduzível; fuzz prolongado fora de PR | Baixo a médio |
| Dependências podem conter vulnerabilidades alcançáveis | `govulncheck` cruza advisories com caminhos de chamada para reduzir ruído[^19] | Go vulnerability database | Rodar em PR/agenda, armazenar saída e exigir triagem por alcançabilidade e contexto | `govulncheck ./...` agendado antes de torná-lo gate | Fixture de finding conhecido no pipeline e registro de falso positivo/aceite de risco | Baixo |
| Workflows usam actions por tags maiores e precisam de permissões mínimas | OpenSSF Scorecard verifica token permissions e dependências fixadas; recomenda permissões read-only por padrão e pins por hash[^20] | Scorecard checks atuais | Pin de action por SHA com comentário de versão, `permissions: contents: read` no topo e write somente por job de release | Aplicar primeiro a releases, depois todos os workflows | Scorecard, zizmor/actionlint e inspeção de permissões efetivas | Baixo |
| Imagens, binários e chart precisam ser associados ao commit e ao build oficial | SLSA 1.2 define provenance; Build L1 documenta origem, L2 exige provenance assinada por plataforma hospedada; verificação deve conferir subject digest, builder e parâmetros[^21] | SLSA 1.2 | Gerar SBOM e provenance para cada artefato, publicar por digest e documentar verificação | Provenance L1 e checksums assinados na primeira etapa | Consumidor verifica digest da imagem/chart/binário contra provenance e origem esperada | Médio |
| Assinatura existente não é útil sem política de verificação do consumidor | Cosign suporta assinatura OIDC keyless e verificação por identidade/issuer; attestations podem ser anexadas ao artefato OCI[^22] | Sigstore/Cosign estável fixado | Assinar imagens e chart OCI; documentar `cosign verify` com workflow identity e issuer; associar SBOM/provenance | Assinar somente imagens inicialmente | Job independente baixa artefato publicado e verifica assinatura, identity, issuer, digest e attestations | Médio |

## Arquitetura-alvo incremental

### 1. Um contrato canônico de alvo

O núcleo deve operar sobre uma identidade que não possa colidir:

```text
TargetIdentity {
  apiVersion
  kind
  namespace
  name
  uid
}
```

`uid` é resolvido no momento da decisão e revalidado imediatamente antes de mutar ou restaurar. A seleção declarativa pode omitir UID, porque ela descreve intenção futura; o conjunto resolvido e o snapshot não podem omiti-lo. A UI pode continuar exibindo `namespace/name`, mas URLs, audit events e requests preservam kind e uma chave opaca ou codificada sem perda.

O escopo deve ser uma união explícita. `all` precisa ser opt-in literal; ausência de seletor é inválida. Referências exatas, namespaces, grupos e label selectors não devem ser compactados em listas paralelas. A resolução produz uma coleção ordenada de identidades, com motivo de inclusão/exclusão e hash da entrada. Preview e execução usam esse mesmo resultado ou recusam executar quando resourceVersion/UID mudou.

### 2. Uma operação durável de power lifecycle

O snapshot é parte de uma transação lógica que atravessa duas stores: API Kubernetes do Aura Power e workload externo. Como não há transação ACID entre elas, a segurança vem de estados duráveis, operações idempotentes e compensação.

Fluxo recomendado:

1. Ler target e workload; validar UID, generation, guardrails e autoridade de campos.
2. Criar ou atualizar uma operação com snapshot imutável e `observedGeneration`.
3. Confirmar que a operação foi persistida.
4. Aplicar um patch mínimo, condicionado ao UID/resourceVersion observado.
5. Observar a convergência do recurso real e gravar `Applied` ou `Failed`.
6. Na restauração, revalidar UID e ownership, aplicar apenas o campo controlado e observar prontidão.
7. Marcar `RestoreVerified`; reter evidência conforme política, em vez de apagar imediatamente o único snapshot.

Uma primeira entrega pode manter a operação em `PowerTarget.status`. Se histórico, retenção, múltiplas operações ou aprovação exigirem independência, um `PowerOperation` próprio torna o lifecycle mais claro. Essa nova CRD só se justifica depois de testes provarem que conditions no target são insuficientes.

### 3. Adaptadores por capacidade e autoridade

`DeploymentReplicas`, `StatefulSetReplicas` e `CronJobSuspend` devem ser capacidades separadas. Cada adaptador declara campos lidos, campos escritos, snapshot mínimo, convergência e conflitos conhecidos. HPA, KEDA, Argo CD e Flux entram como detectores/compatibility policies, sem ficarem misturados ao motor temporal.

Uma matriz de autoridade acompanha cada decisão:

| Campo | Aura Power | GitOps | Autoscaler | Condição para mutação |
|---|---|---|---|---|
| Deployment/StatefulSet `spec.replicas` | Temporária, durante janela | Pode declarar valor base | Pode ajustar continuamente | Integração explicitamente admitida; conflito testado |
| CronJob `spec.suspend` | Temporária | Pode declarar valor base | Não aplicável | Estado anterior capturado; política de missed Jobs definida |
| Imagem/config/template | Somente leitura | Autoridade principal | Somente leitura | Mudança deve continuar chegando durante janela off |
| Labels/annotations | Apenas chaves próprias | Pode declarar outras | Pode declarar outras | Patch preserva campos não relacionados |

Isso permite explicar por que um alvo está bloqueado, em vez de tratar toda annotation Argo CD como prova suficiente de ownership.

### 4. Um lifecycle observável de ponta a ponta

Cada ação deve ter `operationID`, `requestID`, actor, policy/override vencedor, target identity, generation, timestamps, estado e resultado. O mesmo identificador acompanha resposta HTTP, status CRD, log estruturado, métricas e audit event.

Métricas precisam distinguir:

- decisão bloqueada de execução falha;
- ação aceita de workload convergido;
- duração de decisão, aplicação e prontidão;
- retries/conflitos de resultados finais;
- restauração solicitada de restauração verificada.

Cardinalidade fica controlada: IDs e nomes de workload pertencem a logs/auditoria; métricas usam kind, resultado, razão estável e integração, sem target name. Alertas se baseiam em operações presas, restauração falha, divergência de UID e crescimento de conflitos.

### 5. Um produto HTTP/UI coerente

OpenAPI deve definir requests, responses, paginação, erros e RBAC. O frontend consome tipos gerados e um transporte único. A interface mostra estados separados para carregando, vazio, indisponível, forbidden, validação, conflito e stale generation.

A jornada central deve apresentar a mesma sequência do controller: seleção resolvida, preview, confirmação, aceite, execução, efeito e recuperação. “Success” é reservado para efeito observado. Aprovação, quando ativa, mostra pedido, decisão do approver e aplicação como estados separados.

## Restrições para migração de contratos públicos

Nenhuma refatoração deve alterar silenciosamente os contratos atuais. O inventário mínimo de consumidores inclui:

- objetos `power.aura.sh/v1alpha1` armazenados em clusters;
- manifests GitOps e exemplos YAML;
- nomes e paths HTTP consumidos por painel, CLI e integrações;
- saída humana e estruturada da CLI;
- values Helm, nomes de Secret, labels e RBAC;
- métricas, audit events e dashboards externos.

Cada mudança pública precisa de uma decisão explícita entre adição compatível e nova versão. A sequência segura é:

1. Publicar o contrato novo como campos aditivos ou nova versão servida em paralelo.
2. Fazer server/controller aceitarem antigo e novo, com conversão sem perda.
3. Atualizar CLI e frontend para produzir o novo contrato, mantendo leitura do antigo.
4. Expor warning e telemetria sanitizada sobre uso legado.
5. Oferecer backup, dry-run de migração, rollback e documentação de upgrade.
6. Migrar storage e confirmar `status.storedVersions` antes de deixar de servir a versão antiga.[^4]
7. Remover compatibilidade apenas numa versão major anunciada.

Para identidade, um campo novo `targetRef.uid` pode ser opcional em objetos declarativos, mas deve ser obrigatório no status resolvido e na operação. Para escopo, o formato antigo pode converter para o novo somente quando a correspondência for inequívoca. Listas separadas que produzam produto cartesiano precisam de warning ou rejeição; escolher silenciosamente uma interpretação é perda de significado.

Mudanças de URL devem preservar aliases por um ciclo de depreciação. Saída JSON/YAML da CLI requer versionamento ou garantia de compatibilidade; texto humano pode evoluir, desde que scripts sejam direcionados ao modo estruturado. Renomear values Helm exige aceitar chave antiga com validação de conflito e warning. Métricas renomeadas devem coexistir durante uma janela documentada, porque dashboards e alertas são consumidores públicos.

## Roadmap priorizado

### P0 — Provar contenção antes de ampliar testes mutantes

Objetivo: impedir atuação fora do conjunto resolvido e garantir recuperação independente.

- Fixar identidade completa e bloquear ação/restauração quando UID divergir.
- Rejeitar escopo vazio e testar referências exatas sem produto cartesiano.
- Persistir snapshot antes da mutação e impedir sobrescrita em reconciliação repetida.
- Restringir recursos de controle ao namespace efetivo da instalação.
- Criar harness com allowlist, baseline, watchdog de restauração e limpeza por run ID.

Gate de saída: testes determinísticos passam para homônimos, objeto recriado, conflito de status e queda entre cada etapa; Kind demonstra que nenhum objeto fora das fixtures foi alterado.

### P1 — Tornar recuperação e GitOps comportamentos demonstrados

Objetivo: fazer o lifecycle sobreviver à realidade de um cluster.

- Introduzir conditions, `observedGeneration` e operation ID.
- Restaurar CronJob pelo snapshot e definir tratamento de Jobs ativos/missed runs.
- Aplicar patches mínimos com tratamento explícito de `409`.
- Executar a matriz Argo CD: tracking, self-heal, `RespectIgnoreDifferences`, criação inicial, ApplicationSet e mudança legítima no Git.
- Classificar HPA/KEDA/Flux como bloqueado, experimental ou suportado com base em testes.

Gate de saída: ciclos off/on, reinício e leader change convergem; restauração preserva estado anterior; não há oscilação contínua com a configuração GitOps suportada.

### P1 — Fechar contratos de autenticação e aprovação

Objetivo: nenhuma falha de dependência ou lacuna de UI produzir acesso ou sucesso aparente.

- Fazer o frontend falhar fechado e unificar estados de transporte.
- Separar access/refresh tokens, rotação, revogação e invalidação por alteração de usuário.
- Implementar a state machine de aprovação de ponta a ponta ou retirar a capacidade da navegação/documentação até estar pronta.
- Aplicar matriz RBAC no servidor a todos os verbos e recursos.

Gate de saída: ASVS 5.0.0 é usado como checklist versionado; testes negativos passam em API e browser; aprovação concorrente aplica no máximo uma operação autorizada.

### P2 — Unificar semântica e experiência

Objetivo: uma única linguagem entre domínio, API, CLI, UI e auditoria.

- Publicar OpenAPI e gerar tipos/clientes.
- Tornar preview/explain/execution projeções do mesmo motor e conjunto resolvido.
- Adicionar paginação, ordenação estável e cancelamento.
- Executar acessibilidade WCAG 2.2 AA com automação e avaliação manual, sem alegar conformidade apenas pelo axe.
- Medir bundle e Core Web Vitals antes de dividir chunks.

Gate de saída: golden contracts e jornadas browser cobrem todas as páginas, personas e estados; nenhum adaptador perde kind, UID, timezone, ausência, zero ou erro.

### P2 — Qualidade de engenharia e portabilidade

Objetivo: detectar regressões no nível mais barato que mantém a evidência necessária.

- Unit/property/fuzz no domínio; `-race`, vet/lint e `govulncheck`.
- envtest com CRDs, status, webhooks, conflicts e watches.
- matriz Kind com versões Kubernetes declaradas, Helm, persistence, recovery e Argo CD.
- EKS manual somente com fixtures admitidas, mesma imagem por digest e evidência sanitizada.
- Testes Playwright isolados em Chromium por PR e matriz ampliada em Firefox/WebKit/mobile.

Gate de saída: cada capacidade da matriz aponta para teste, ambiente, dependências reais/simuladas e evidência; falha conhecida permanece visível.

### P3 — Maturidade open source e cadeia de fornecimento

Objetivo: permitir que contribuidores reproduzam resultados e consumidores verifiquem artefatos.

- Pin de GitHub Actions por SHA, permissões mínimas, CODEOWNERS e branch protection.
- Dependabot/Renovate com política de compatibilidade e revisão.
- SBOM, checksums, imagens/chart por digest, Cosign e provenance SLSA.
- Chart test, `values.schema.json`, documentação de upgrade/rollback e compatibility matrix.
- Templates de issue com versão, ambiente, reprodução, esperado/observado, impacto e teste de regressão.

Gate de saída: build oficial publica provenance e artefatos assinados; um job independente os baixa e verifica identidade, issuer, digest e attestations.

## Comparação com projetos do domínio

O kube-green é uma referência ativa de forma e manutenção, não uma especificação para copiar. Seu repositório público usa CRD própria, IANA time zones, exclusões explícitas, Kind no desenvolvimento e E2E separados para instalação por Helm e kustomize.[^23] Isso sustenta três escolhas para o Aura Power: um contrato Kubernetes nativo pequeno, cenários reais de cluster e instalação testada por mais de uma forma quando anunciada.

O kube-downscaler original no GitHub está arquivado desde 2020 e aponta para sua continuação no Codeberg.[^24] Ele continua útil como histórico do padrão de downscaling, mas não deve ser tratado como baseline atual de manutenção, segurança ou compatibilidade. Comparações futuras precisam fixar commit/release e verificar a implementação atual na origem indicada.

Aura Power tem ambição maior que um downscaler por annotation: políticas, overrides, preview/explain, auditoria, autenticação, painel, métricas e notificações ampliam a superfície de contratos. A consequência prática é evitar uma refatoração monolítica. O caminho mais seguro preserva o motor de decisão puro, fortalece identidade/operação primeiro e migra adaptadores públicos em ondas pequenas com testes de compatibilidade.

## Estratégia de validação permanente

| Camada | Frequência | Dependências reais | Propriedade comprovada | Não comprova |
|---|---|---|---|---|
| Unit/property | Cada PR | Relógio e domínio em memória | Invariantes, prioridades, escopo, DST, ausência/zero | Kubernetes API e concorrência externa |
| Fuzz | Seeds em PR; campanha agendada | Parser/conversores reais | Robustez a entrada e corpus de regressão | Semântica distribuída completa |
| Go race | Cada PR | Código concorrente exercitado | Data races nos caminhos cobertos | Ausência universal de races |
| Contract tests | Cada PR | Serialização CRD/HTTP/CLI/UI | Compatibilidade e ausência de perda | Controllers nativos e workload real |
| envtest | Cada PR | etcd e kube-apiserver | Schema, admission, status, conflicts, watches | Pods, Jobs, readiness, garbage collection[^2] |
| Kind | PR smoke + ampliado | Kubernetes completo e imagens reais | Helm, workloads, recovery, GitOps, restart | Particularidades de IAM/EKS |
| Playwright | PR Chromium; ampliado multi-browser | Server real e fixture isolada | Jornada do usuário e efeito observável | Toda acessibilidade manual[^16] |
| EKS | Manual controlado | Infraestrutura de produção, fixture dedicada | Compatibilidade real admitida | Aplicações de negócio não testadas |

Flakiness deve ser dado. Playwright diferencia `passed`, `flaky` e `failed` quando retries estão ativos.[^15] A CI pode coletar essa classificação, mas um teste crítico flaky não deve liberar a mudança. Retry serve para obter evidência adicional; a correção deve atacar isolamento, espera observável, relógio, dependência ou cleanup.

## Segurança e distribuição como critérios verificáveis

ASVS 5.0.0 oferece requisitos identificáveis e versionados para web apps e serviços.[^13] A campanha deve mapear controles pertinentes, não declarar “conforme ASVS” de forma genérica. O mínimo cobre validação de entrada, autenticação, sessão/token, autorização, logging sem segredos, arquivos/dados armazenados, comunicação e API.

OpenSSF Scorecard é um radar para práticas do repositório. O próprio projeto descreve limitações das verificações; por exemplo, sinais de configuração não provam que a rotina é operada corretamente.[^20] O resultado deve gerar backlog e evidência, sem se transformar em objetivo de pontuação isolada.

SLSA separa existência de provenance, assinatura por plataforma hospedada e hardening do builder.[^21] A recomendação inicial é chegar a provenance gerada automaticamente para imagens, chart e binários, então adicionar assinatura e verificação externa. A afirmação de nível só deve ocorrer depois de avaliar todos os requisitos da versão 1.2 aplicável.

Cosign permite assinatura keyless com identidade OIDC e anexação de attestations a artefatos OCI.[^22] O ganho aparece quando o consumidor verifica uma identidade e issuer esperados, além do digest. Documentação que mostra apenas `cosign verify` sem restrições de identidade é incompleta.

## Decisões recomendadas

1. **Não reescrever o produto agora.** As falhas mais graves têm fronteiras identificáveis: target identity, scope resolution, durable operation e namespace de controle. Corrigi-las em sequência reduz risco e produz contratos testáveis.
2. **Criar uma nova versão de CRD quando a identidade/seleção não puder ser expressa de modo compatível.** Kubernetes oferece serving paralelo e conversão; usar isso é mais seguro que reinterpretar objetos existentes.[^4]
3. **Tratar compatibilidade GitOps como capacidade versionada.** Publicar a matriz exata de Argo CD e configurações aprovadas; HPA, KEDA e Flux só mudam de experimental para suportado após testes.
4. **Reservar “concluído” para efeito observado e recuperação verificada.** HTTP 2xx, status atualizado ou audit event isolado não encerram a operação.
5. **Usar EKS para aceitação, não descoberta destrutiva.** Falhas e disputa deliberada são reproduzidas primeiro no Kind; EKS usa somente fixtures, allowlist e watchdog independente.
6. **Entregar segurança da cadeia junto com a qualidade funcional.** Artefato testado deve ser o mesmo digest assinado e promovido; caso contrário, a evidência não acompanha a entrega.

## Fontes

[^1]: Kubebuilder, “[Good practices](https://book.kubebuilder.io/reference/good-practices.html),” documentação oficial, consultada em 12 set. 2026; Kubernetes, “[Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),” documentação oficial.
[^2]: Kubebuilder, “[Configuring envtest for integration tests](https://book.kubebuilder.io/reference/envtest.html),” documentação oficial, consultada em 12 set. 2026.
[^3]: Kubernetes, “[Owners and Dependents](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/),” documentação oficial; Kubernetes, “[Object Names and IDs](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/),” documentação oficial.
[^4]: Kubernetes, “[Versions in CustomResourceDefinitions](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definition-versioning/),” e “[Custom Resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/),” documentação oficial.
[^5]: Kubernetes, “[CronJob](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/),” documentação oficial.
[^6]: Kubernetes, “[Kubernetes API Concepts](https://kubernetes.io/docs/reference/using-api/api-concepts/),” documentação oficial.
[^7]: Helm, “[Chart Best Practices Guide](https://helm.sh/docs/chart_best_practices/),” e “[Values](https://helm.sh/docs/chart_best_practices/values/),” documentação oficial do Helm 3.
[^8]: TypeScript, “[strict](https://www.typescriptlang.org/tsconfig/strict.html)” e “[noUncheckedIndexedAccess](https://www.typescriptlang.org/tsconfig/noUncheckedIndexedAccess.html),” referência oficial TSConfig.
[^9]: Argo CD, “[Respect Ignore Differences Configs](https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/#respect-ignore-differences-configs),” documentação oficial.
[^10]: Argo CD, “[Resource Tracking](https://argo-cd.readthedocs.io/en/stable/user-guide/resource_tracking/),” documentação oficial.
[^11]: Argo CD, “[Automated Sync Policy](https://argo-cd.readthedocs.io/en/stable/user-guide/auto_sync/),” documentação oficial.
[^12]: Helm, “[Chart Tests](https://helm.sh/docs/topics/chart_tests/),” documentação oficial.
[^13]: OWASP, “[Application Security Verification Standard 5.0.0](https://github.com/OWASP/ASVS/tree/v5.0.0),” maio de 2025; [repositório oficial](https://github.com/OWASP/ASVS).
[^14]: React, “[StrictMode](https://react.dev/reference/react/StrictMode),” “[Rules of React](https://react.dev/reference/rules)” e “[useEffect](https://react.dev/reference/react/useEffect),” documentação oficial.
[^15]: Playwright, “[Best Practices](https://playwright.dev/docs/best-practices),” “[Locators](https://playwright.dev/docs/locators)” e “[Retries](https://playwright.dev/docs/test-retries),” documentação oficial.
[^16]: Playwright, “[Accessibility testing](https://playwright.dev/docs/accessibility-testing),” documentação oficial; W3C, “[Web Content Accessibility Guidelines (WCAG) 2.2](https://www.w3.org/TR/WCAG22/),” recomendação oficial.
[^17]: Go, “[Go Fuzzing](https://go.dev/doc/security/fuzz/),” documentação oficial.
[^18]: Go, “[Data Race Detector](https://go.dev/doc/articles/race_detector),” documentação oficial.
[^19]: Go, “[Find and fix vulnerable dependencies with govulncheck](https://go.dev/doc/tutorial/govulncheck),” documentação oficial.
[^20]: OpenSSF Scorecard, “[Checks](https://github.com/ossf/scorecard/blob/main/docs/checks.md)” e “[Beginner checks](https://github.com/ossf/scorecard/blob/main/docs/beginner-checks.md),” repositório oficial.
[^21]: SLSA, “[Specification 1.2](https://slsa.dev/spec/v1.2/),” “[Build Track Basics](https://slsa.dev/spec/v1.2/build-track-basics)” e “[Verifying artifacts](https://slsa.dev/spec/v1.2/verifying-artifacts),” especificação aprovada.
[^22]: Sigstore, “[Signing Containers](https://docs.sigstore.dev/cosign/signing/signing_with_containers/)” e “[Verifying Signatures](https://docs.sigstore.dev/cosign/verifying/verify/),” documentação oficial do Cosign.
[^23]: kube-green, “[kube-green repository](https://github.com/kube-green/kube-green),” repositório oficial, consultado em 12 set. 2026.
[^24]: hjacobs, “[kube-downscaler](https://github.com/hjacobs/kube-downscaler),” repositório oficial arquivado, com continuação indicada pelo mantenedor no Codeberg.
