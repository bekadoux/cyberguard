# Установка Go Backend Handoff (локально, Windows + WSL2)

Исходная версия проекта: `main` / `7b91f0b`. **Это исходный код для review, а не протестированный production release.**

## Перед заменой

1. В Visual Studio 2026 открой текущий репозиторий `D:\source\repos\AISaPU_project`.
2. Убедись, что актуальная `main` соответствует `7b91f0b` или проверь новые изменения команды, если база уже новее. Старый Stash не восстанавливай.
3. Создай новую ветку **из `main`**: `feature/go-backend-handoff`. Если Visual Studio предупредит о незакоммиченных изменениях, сначала безопасно сохрани их в Stash, **не выбирай Discard**.
4. Сделай резервную копию рабочей папки. Не изменяй и не удаляй `.git`, `.vs`, `.local`, файлы `.env`, секреты и Docker volumes.

## Замена файлов

Предпочтителен `cyberguard-go-handoff-patch.zip`: распакуй его **с сохранением вложенных путей** в `D:\source\repos\AISaPU_project` с заменой совпадающих файлов. Полный `cyberguard-go-handoff-full.zip` предназначен для отдельной копии/рабочего дерева, если нужно увидеть весь результат целиком.

Пример: `backend/go/internal/httpapi/handoff_workflow.go` должен появиться в одноимённой папке проекта, а не в `AISaPU_project/cyberguard_handoff_implemented/...`.

После замены проверь в терминале WSL2:

```sh
git branch --show-current
# Должно быть feature/go-backend-handoff
git diff --stat
git status --short
```

Если Git на `/mnt/d` опять показывает массовые изменения CRLF/LF, не запускай `git add .`, `git restore` или `git reset --hard`. Сначала сравни `git diff --ignore-space-at-eol --stat`.

## Перед запуском приложения

В этой версии появляются две миграции:

- `db/migrations/identity/00003_user_admin_audit.sql`
- `db/migrations/incidents/00003_context_and_investigations.sql`

Их **нужно применить обоими соответствующими migrator-пользователями** в существующих локальных PostgreSQL. Docker migration image содержит копии SQL на этапе сборки — сначала его пересобери. Следуй `docs/docker-setup.md` / Makefile с имеющимися секретами; не создавай пароли заново и не удаляй database volumes. Прежде чем запускать миграции, проверь их с архитектором и сделай backup локальных данных.

После миграций пересобери Go API, поскольку появились новые endpoints. AI Worker/Python и модели менять не нужно, но конечный интеграционный тест необходимо повторить из-за нового immutable snapshot-контракта.

## Проверки

- `make go-sqlc` — с Go 1.27.3 и сетевым доступом регенерировать sqlc-модели для новых таблиц, затем проверить diff. Генерируемые файлы вручную не редактировать.
- `make go-check`, `make go-build`, `make go-test-integration` — последнее только на **двух отдельных одноразовых тестовых БД**.
- `PYTHONPATH=backend/python python3 -m pytest -q backend/python/tests` — Python-тесты.
- `python3 scripts/demo-incident-smoke.py` — предварительный сухой прогон тестового инцидента, `--apply` только в dev и при передаче пароля через защищённый файл.
- Проверить роли, автоназначение, комментарии, историю, уведомления, администрирование, автоматическое закрытие и Go↔Python AI end-to-end.

**Не выполнять `git commit` и тем более `git push` до тестирования и согласования с командой.**

Полный перечень маршрутов и ограничений: `api/go-handoff.openapi.yaml` и `docs/handoff-implementation.md`.
