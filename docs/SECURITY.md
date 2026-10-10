# Безопасность сервера: модель угроз, инварианты, реестр

Живой документ серверной очереди (срезы 18–29, D-138). Каждый срез этой очереди начинает с чтения этого файла и заканчивает его обновлением: новые инварианты с тестом, закрытые угрозы, новые находки. Если код и этот файл расходятся, прав код, файл чинится в том же коммите.

## Что защищаем

| Актив | Почему важно |
|---|---|
| Аккаунты и сессии | Чужой вход = действия от имени человека или организации |
| Профили, отклики, рекомендательные письма, файлы | Персональные данные (152-ФЗ); рекомендацию не должен видеть соискатель, отклик не должен видеть чужой работодатель |
| Права в организациях | Кто публикует вакансии, видит отклики, приглашает от имени организации |
| Почта (очередь писем) | Письмо не тому адресату = утечка; массовая рассылка от нашего имени = блокировка домена |
| Доступность | Сайт работает для всей науки РФ (D-138): ~350 тыс. людей, до ~500 запросов в секунду в пике |

## Кто нападает

- **Гость из интернета:** перебор паролей и почт, перебор адресов объектов, тяжёлые запросы, огромные тела, вредные файлы, межсайтовые запросы.
- **Вошедший человек:** чужие профили, отклики, файлы, рекомендации; действия сверх своей роли в организации.
- **Сотрудник организации:** данные кандидатов чужих вакансий и подразделений, профили, скрытые от его организации (срез 15).
- **Тот, кто перехватил ссылку из письма:** одноразовые токены подтверждения, сброса, рекомендации, приглашения.
- **Цепочка поставок:** уязвимые версии Go и библиотек.

## Границы доверия

1. Браузер → сервер (`/api`): всё входящее недоверенное. Проверки: `auth.SameOrigin`, `auth.Authenticate`, лимиты тела, разбор JSON (`apierr.DecodeJSON`).
2. Прокси → сервер: `X-Forwarded-For` принимается только от адресов из `SCIBOX_TRUSTED_PROXIES` и читается справа налево до первого недоверенного; без списка IP берётся из соединения (`TestMetaTakesTheIPFromTheConnectionNotFromHeaders`, `TestClientAddr`).
3. Сервер → PostgreSQL: только параметризованные запросы sqlc.
4. Сервер → Crossref, SMTP: ответы внешних сервисов недоверенные; у каждого обращения есть срок, число одновременных ограничено, серия сбоев останавливает обращения (срез 25, `internal/resilience`).
4а. Сервер → хранилище файлов (S3) и ClamAV (срез 23): файл недоверен, пока ClamAV не ответил «чисто»; «не знаем» не значит «чисто» (`antivirus.ErrUnavailable`); хранилище отвечает только по ключам, которые мы сами выдали; его сбой не выдаётся за «нет файла».
5. Письма → человек: ссылки с одноразовыми токенами, в базе только хеш.
6. Сервер → внутренний порт (срез 26): `/metrics`, `/debug/pprof` и проверки готовности лежат на отдельном адресе `SCIBOX_OPS_ADDR`, а не на публичном порту; в бою адрес не из чужих сетей или с секретом `SCIBOX_OPS_TOKEN` (`opsserver`, `config.checkObservability`).
7. Сервер → GlitchTip (срез 26): отчёты об ошибках покидают сервер и хранятся в чужой системе 30 дней; уходит только очищенное и разрешённое (`errreport`, D-172).

## Инварианты (что всегда верно и каким тестом доказано)

Новый инвариант добавляется только вместе с тестом, который упадёт при его нарушении.

| ID | Инвариант | Доказательство |
|---|---|---|
| I-01 | Пароли хранятся только как argon2id; параметры читаются из хеша | `auth`: `TestHashAndVerify`, `TestVerifyUsesParamsFromHash`, `TestVerifyRejectsMalformedHashes` |
| I-02 | Токены (сессии, ссылки из писем) случайные, в базе только SHA-256 | `TestNewTokenIsRandomAndHashed` |
| I-03 | Одноразовая ссылка срабатывает один раз даже при гонке | `TestConfirmEmailLinkWorksOnlyOnceEvenWhenRacing`, `TestResetPasswordLosesTheRaceForTheLink`, `TestInvitationLosesTheRaceForTheLink`, `TestAnswerLosesTheRaceForTheLink` |
| I-04 | Вход по несуществующей почте тратит столько же времени, сколько по существующей | `TestDummyHashIsARealHash` |
| I-05 | Лимиты попыток входа, писем, приглашений, откликов хранятся в базе и работают при нескольких копиях сервера | `TestHTTPRateLimit`, `TestRegisterMailRateLimitPerIP`, `TestInviteIsRateLimited`, `TestApplyRateLimit` |
| I-06 | Изменяющий запрос с чужого сайта отклоняется | `TestHTTPSameOriginProtection` |
| I-07 | Cookie сессии: HttpOnly, SameSite=Lax, Secure при https | `TestHTTPCookieIsSecureOverHTTPS` |
| I-08 | Внутренние ошибки не показываются человеку (500 без подробностей) | `TestHTTPInternalErrorsAreHidden`, `TestHTTPServerFailuresAreHiddenFromThePerson` |
| I-09 | Сбой базы никогда не превращается в «успех» или «нет данных» | `TestDatabaseFailuresAreNeverSwallowed` (в каждом пакете критичной зоны) |
| I-10 | Отказ в правах ничего не меняет в базе | `TestForbiddenActionsChangeNothing` |
| I-11 | Каждая пара «роль × действие» проверена явно | `TestWhoCanDoWhat` (`access`, `orgs`, `vacancies`), `TestWhoSeesWhat` |
| I-12 | Файл неизвестного вида никогда не выдаётся; выдача с `CSP: sandbox` и `nosniff` | `TestUnknownFileKindIsNeverServed`, `TestHTTPFileDownload` |
| I-13 | Одно событие = одно уведомление, даже при одновременных действиях | `TestSimultaneousDecisionsPassOnce`, `TestSimultaneousAnswersPassOnce`, `TestSimultaneousOpeningsNotifyOnce` |
| I-14 | Письмо из очереди, взятое упавшим отправителем, возвращается в очередь | `TestExpiredLeaseReturnsMailToQueue` |
| I-15 | Публичные страницы организаций не содержат почт и номеров аккаунтов | тесты `orgs` (срез 4) |
| I-16 | Число из запроса не переворачивается при переводе в меньший тип: огромный номер страницы даёт пустой список, а не 500 | `internal/num`: `TestInt16`, `TestInt32`, `TestUint32`; HTTP: huge offset в `orgs`, `vacancies`, `notifications`, `applications`, `matching` |
| I-17 | Каждый `//nolint` называет правило и причину; в коде 0 замечаний линтера | `nolintlint` в `server/.golangci.yml`, `make lint` в `make test` и CI |
| I-18 | Ни один адрес не читает тело больше своего лимита (1 МиБ, загрузки 64 МиБ); заявленная длина больше лимита — 413 до обработчика | `TestPerimeter/every_route_limits_the_request_body` (все адреса роутера), `TestBodyLimits` |
| I-19 | Каждый ответ сервера несёт CSP с `frame-ancestors 'none'` и `nosniff`; у сайта нет `unsafe-inline` и чужих адресов | `TestSecurityHeaders`, `TestPerimeter/security_headers_on_every_kind_of_response` |
| I-20 | X-Forwarded-For принимается только от доверенных прокси; доверять всему интернету (`/0`) нельзя | `TestClientAddr`, `TestParseProxies` |
| I-21 | Ограничение частоты общее для всех копий сервера; сбой счётчиков не пропускает запрос | `TestPostgresStore`, `TestPerimeter/public_reads…` (вторая копия видит тот же счёт), `TestThrottleStoreFailure` |
| I-22 | Нулевой или отрицательный срок соединения не означает «без срока» | `TestNewServerSetsEveryTimeout` |
| I-23 | Боевой режим не стартует с настройками для локальной работы и не печатает пароль базы | `TestProductionRefusesUnsafeSettings` |
| I-24 | Каждый адрес из таблицы периметра существует в роутере и находится по адресу запроса | `TestPerimeter/every_listed_route_exists_in_the_router`, `TestThrottleSubrouterRoots` |
| I-25 | Смена почты, удаление аккаунта и выдача роли владельца разрешены только со свежим паролем (10 минут после ввода в этом сеансе); вход по ссылке из письма свежим не считается | `TestFreshPassword`, `TestRequestEmailChangeNeedsFreshPassword`, `TestRequestDeletionNeedsFreshPassword`, `TestOwnerRoleNeedsFreshPassword`, `TestHTTPOwnerNeedsFreshPassword` |
| I-26 | Смена и сброс пароля, откат почты и просьба удалить аккаунт завершают сеансы; вход всегда даёт новый сеанс и завершает прежний сеанс этого браузера | `TestChangePassword…`, `TestResetPassword…`, `TestRevertEmailChange`, `TestDeletionLifecycle`, `TestHTTPLoginReplacesTheBrowserSession`, e2e `account-security.spec.ts` |
| I-27 | После стирания аккаунта ни одна таблица не ссылается на него, кроме обезличенных откликов и записей «кто сделал»; новая таблица со ссылкой на аккаунт без правила стирания роняет тест | `TestEveryReferenceToTheAccountIsHandled`, `TestEraseRemovesPersonalData` |
| I-28 | Стёртый аккаунт не входит и не восстанавливается: адрес-заглушку нельзя ввести ни в одну форму, сеанс стёртого не принимается | `TestDeletionLifecycle`, `GetSessionByTokenHash` (`deleted_at IS NULL`) |
| I-29 | Пароль из списка частых (NCSC 100k, от 10 знаков) не принимается, в том числе набранный в русской раскладке; испорченный список останавливает сервер при запуске | `TestCommonPasswordList`, `TestCommonPasswordInRussianLayout`, `TestLoadCommonPasswordsRejectsBrokenData` |
| I-30 | У каждого адреса роутера есть строка в таблице доступа; для каждого вида человека (гость, посторонний, субъект, владелец, кадровик, руководитель своего и чужого подразделения, владелец другой организации) ответ совпадает с таблицей; отказ ничего не меняет в базе | `cli.TestAccessTable` (103 адреса) |
| I-31 | Чужому закрытому объекту отвечают так же (код и тело), как на выдуманный номер; 403 только у публичных объектов | `TestAccessTable` (строки с `twin`), `TestAcceptByIDAndMyInvitations` |
| I-32 | Журнал действий только дописывается: нельзя изменить, удалить, очистить; можно только стереть адрес | `audit.TestTheLogIsAppendOnly` |
| I-33 | Действие и запись о нём в одной транзакции: журнал не принял запись — действие не прошло, файл отклика не выдан | `Test…FailWhenTheJournalIsDown` в `orgs`, `vacancies`, `applications`, `offers`, `auth`; `audit.TestRecordRollsBackWithTheAction`; `TestRefused…LeaveNoTrace` (отказ не пишет) |
| I-34 | Журнал организации читает только её владелец; запрос журнала не выходит за организацию | `TestHTTPAuditLog`, строка `GET …/audit` таблицы доступа, `audit.TestRecordAndList` |
| I-35 | У организации с сотрудниками всегда есть владелец: приём приглашения и стирание владельца не разминутся | `orgs.TestAcceptingWaitsForTheErasureOfTheOwner`, `TestInvitationsToAnOrganizationWithoutOwner` |
| I-36 | Каждое действие организации в журнале имеет подпись на сайте | `audit.TestEveryOrganizationActionHasALabelOnTheSite` |
| I-37 | Весь журнал сервера проходит через маску: логгер создаётся в одном месте (`cli.newLogger`), стандартный `log` и `slog.Default` тоже; в рабочем коде нет записи в журнал в обход маски | `logmask.TestNoLoggerBypassesTheMask`, `cli.TestEveryLoggerGoesThroughTheMask` |
| I-38 | Почты, адреса людей (IPv4 до /24, IPv6 до /48), токены, пароли и значения под ключами `password`/`token`/`cookie` не попадают в журнал на любом уровне и любым способом передачи (сообщение, строка, ошибка, группа, `With`, `LogValuer`) | `logmask.TestHandlerHidesEverySecretOnEveryLevelAndEveryCarrier`, `TestHandlerHidesTheValueOfSensitiveKeys`, `TestMask` |
| I-39 | Срок хранения никогда не значит «без срока» и не опускается ниже пола; журнал действий удаляется только внутри объявленной чистки и только записи старше 90 дней, как бы ни был настроен код | `retention.TestPolicyNormalized`, `TestAuditTriggerProtectsRecentHistoryWhateverTheCodeSays` |
| I-40 | Чистка по срокам идёт партиями, одна копия сервера на вид чистки, сбой одного вида не останавливает остальные и не оставляет половины партии | `retention.TestPassWorksInBatches`, `TestPassSkipsAKindThatAnotherCopyIsWorkingOn`, `TestTwoCopiesCleanTogetherWithoutDuplicatesOrErrors`, `TestPassKeepsGoingAfterOneKindFails`, `TestPassDatabaseFailuresAreNeverSwallowed` |
| I-41 | Текст отправленного (и окончательно не доставленного) письма стирается сразу; ждущее повтора письмо хранит текст | `notifications.TestWorkerSendsEachMailOnce`, `TestWorkerRetriesWithBackoffAndGivesUp` |
| I-42 | После стирания аккаунта ни в одной таблице нет почты, имени, идентификаторов, адреса входа и почт рекомендателей человека; каждая таблица базы описана в карте ПДн | `cli.TestErasedPersonLeavesNothingToRecogniseThemBy`, `erasure.TestPersonalDataMapCoversEveryTable` |
| I-43 | Архив своих данных выдаётся только со свежим паролем, не чаще трёх раз в сутки, только после записи в личный журнал; в нём нет данных других людей, писем рекомендателей о человеке, хешей и секретов | `dataexport.TestExportNeedsAFreshPassword`, `TestExportIsLimitedPerDay`, `TestEveryExportIsInThePersonalJournalAndWithoutAJournalThereIsNoArchive`, `TestArchiveHoldsEverythingAboutThePersonAndNothingAboutOthers`, `TestExportOfAStrangersFileIsImpossible`, `TestHTTPExportWithABrokenDatabaseNeverPassesForASuccess` |
| I-44 | Файл не выдаётся никому, пока ClamAV не ответил «чисто»; заражённого нет ни в хранилище, ни у организации; сбой антивируса не делает файл чистым и не теряет его | `antivirus.TestClamdFailsClosed`, `files.TestVaultPutVirus`, `filework.TestPendingFileBecomesCleanOnlyByTheAnswerClean`, `TestScanFailuresNeverMakeAFileClean`, `applications.TestAFileWaitingForTheCheckIsNotServedToAnyone`, `TestAnInfectedFileIsHiddenFromTheOrganizationAndNeverServed`, `cli.TestFilesThroughTheWholeServerOnTheRealStorageAndAntivirus` (настоящий ClamAV, EICAR внутри PDF) |
| I-45 | Вид файла (PDF по первым байтам) и размер проверяются до записи в хранилище; бесконечный поток обрывается около предела; отказ забирает уже принятые файлы | `files.TestVaultPutRejectsBeforeWriting`, `TestVaultPutSizeLimit`, `TestReadFormErrors`, `TestReadFormTotalSizeLimit`, `applications.TestRefusalsTakeTheFilesBack` |
| I-46 | Содержимое файлов в базу не пишется; старый файл переезжает только после совпадения размера и SHA-256 при чтении обратно | `filework.TestNoFileContentCanBeWrittenToTheDatabaseAgain`, `TestLegacyFilesMoveToTheStorageWithAChecksum`, `TestLegacyMoveRefusesACorruptCopy`, `TestLegacyMoveFailures` |
| I-47 | Запрос с неверными полями или мёртвой ссылкой не доходит до хранилища и антивируса (усиления нагрузки по анонимной ссылке нет) | `files.TestReadFormStopsBeforeTheFilesWhenTheFieldsAreWrong`, `applications.TestHTTPApplyWithWrongFieldsNeverTouchesTheStorage`, `references.TestHTTPLetterIsCheckedBeforeTheFileIsTaken` |
| I-48 | Объекты стёртого аккаунта ставятся в очередь удаления в той же транзакции, что и стирание строк; ключ вычёркивается только после удаления объекта | `erasure.TestEraseRemovesPersonalData`, `filework.TestQueuedObjectsAreDeletedInBatches`, `TestQueuedKeysWaitWhileTheStorageIsDown`, `cli.TestFilesThroughTheWholeServerOnTheRealStorageAndAntivirus` |
| I-49 | Чистка осиротевших удаляет только старые объекты без строки в базе и отказывается работать при подозрительной массе «сирот» (база из старой копии) | `filework.TestSweepRemovesOnlyOldObjectsWithoutARow`, `TestSweepRefusesWhenThereAreSuspiciouslyManyOrphans`, `TestConfigFallsBackToSafeValues` |
| I-50 | Сбой хранилища не выдаётся за «нет файла»; выдача не записывается в журнал, пока файл не открыт; пропавший объект проверенного файла — ошибка, не тихий 404 и не неполный архив | `applications.TestStorageFailuresAreNotNotFoundAndLeaveNoTrace`, `blobstore.TestS3ErrorMapping`, `TestS3BucketGone`, `dataexport.TestArchiveSkipsUncheckedFilesAndFailsOnAMissingObject` |
| I-51 | Боевой режим не стартует без шифрованного канала к хранилищу, с ключами из docker-compose и с выключенным антивирусом; ключи в сообщения не попадают | `config.TestProductionRefusesUnsafeSettings`, `TestLoadFileStorage` |
| I-52 | База сама обрывает запрос дольше `statement_timeout` (10 с) и транзакцию без команд дольше `idle_in_transaction_session_timeout` (30 с); нулевой или отрицательный срок в настройках не значит «без срока» | `dbpool.TestSettingsReachTheDatabase`, `TestStatementTimeoutStopsALongQuery`, `TestIdleTransactionIsCut`, `TestNormalized`, `config.TestLoadDB` |
| I-53 | Запрос, остановленный базой по сроку, даёт 503 `database_busy` с `Retry-After` без подробностей; обрыв запроса самим человеком не считается ошибкой | `apierr.TestWriteInternalDatabaseTimeout`, `dbpool.TestClientCancelIsNotATimeout` |
| I-54 | В журнал медленных запросов попадают имя запроса и время, но не текст запроса и не значения параметров | `dbpool.TestSlowQueriesAreLogged`, `TestQueryName` |
| I-55 | Поиск вакансий и каталог учёных читают и отдают не больше предела (1000 первых результатов), как бы широк ни был запрос и как глубоко ни листали; «больше предела» сообщается явно | `vacancies.TestSearchIsCapped`, `profiles.TestCatalogIsCapped` |
| I-56 | Тексты поиска (`vacancy_search`, `profile_search`) не расходятся с данными: открытость и дата публикации вакансии, коды специальностей, имя, должность и специальности профиля, счётчик статей Q1–Q2 меняются триггерами базы при любом пути записи; закрытая вакансия по тексту и по области не находится | `vacancies.TestSearchRowFollowsTheStatus`, `TestSearchCodesFollowTheSpecialties`, `profiles.TestProfileSearchFollowsTheProfile`, `TestQ12CounterFollowsThePublications`, `journals.TestLoadRecountsTheArticlesInQ1Q2` |
| I-57 | Задача кластера в каждый срок исполняется одной копией и никогда не идёт одновременно в двух; упавшая копия не держит задачу дольше аренды; после сбоя общая для всех копий пауза растёт; паника в задаче не роняет процесс | `jobs.TestClusterJobRunsOncePerIntervalAcrossCopies`, `TestClusterJobIsNeverRunByTwoCopiesAtOnce`, `TestCrashedCopyDoesNotHoldTheJobForever`, `TestFailureBacksOffForAllCopiesAndRecovers`, `TestPanicIsRecoveredAndRecorded`, `TestJobThatOverrunsItsLeaseFails` |
| I-58 | Остановка не обрывает отправляемое письмо; не начатые возвращаются в очередь сразу и без траты попытки; письмо не начинается, если аренда кончится раньше, чем оно уйдёт; задача, оборванная остановкой, не считается упавшей | `notifications.TestShutdownFinishesTheMailInFlightAndReleasesTheRest`, `TestMailIsNotStartedIfTheLeaseWouldEndBeforeItIsSent`, `TestAbortedSendIsNeitherAFailureNorALostMail`, `jobs.TestShutdownLetsTheJobFinishItsPieceAndRecordsSuccess`, `TestInterruptedJobIsReleasedWithoutCountingAsAFailure` |
| I-59 | Две копии сервера и две копии воркера на одной базе: каждое письмо уходит ровно один раз, каждое уведомление создаётся один раз, ни одно не потеряно, даже когда одну копию останавливают посреди работы или расписание обойдено прямыми вызовами | `cli.TestSeveralCopiesNeverSendTwice` (1000 событий, под `-race`), `cli.TestDirectCallsFromSeveralCopiesNeverDuplicate` |
| I-60 | Сбой почтовой службы не тратит попытки писем и не делает их недоставленными; постоянный отказ получателя недоставлено сразу; письмо старше 3 суток после неудачи недоставлено; ошибка прощания не повторяет принятое письмо | `notifications.TestServiceFailureDoesNotSpendTheMailsAttempts`, `TestPermanentRefusalGivesUpAtOnce`, `TestMailThatIsTooOldIsGivenUp`, `mail.TestFailureKinds`, `TestQuitFailureDoesNotUndoAnAcceptedMessage` |
| I-61 | К Crossref одна копия ведёт не больше заданного числа одновременных запросов; после серии сбоев обращений нет, 429 открывает предохранитель сразу, «такого DOI нет» и отмена человеком сбоем не считаются; очередь писем при открытом предохранителе почты ничего не берёт, отказ по письму его не открывает | `resilience` (все переходы, 100%), `crossref.TestGateLimitsConcurrentLookups`, `TestBreakerStopsAskingADeadCrossrefAndRecovers`, `TestNotFoundAndCancelAreNotServiceFailures`, `TestRateLimitAnswerOpensTheBreakerAtOnce`, `notifications.TestBreakerPausesTheQueueAndResumesAfterTheProbe`, `TestMailRefusalsDoNotOpenTheBreaker` |
| I-62 | Нулевой или отрицательный срок, пауза и число в настройках фоновых задач, предохранителя и остановки не значат «без ограничения» | `jobs.TestConfigNormalizedNeverMeansNoLimit`, `resilience.TestNormalized`, `config.TestLoadBackgroundAndShutdown` |
| I-63 | Копия с часами, расходящимися с базой больше допуска, не исполняет никаких задач (не ставит в расписание «на потом» и не берёт письма в аренду на дни вперёд); текст ошибки задачи в базе и в снимке без почт и адресов | `jobs.TestACopyWithAWrongClockRunsNothingUntilItIsFixed`, `TestErrorTextInTheScheduleIsMasked` |
| I-64 | Номер запроса создаёт только сервер; присланный посетителем `X-Request-Id` не используется; номер стоит в заголовке ответа и в каждой записи журнала, написанной во время запроса | `reqid.TestMiddlewareGivesEveryRequestItsOwnNumberAndIgnoresTheClients`, `TestHandlerAddsTheNumberOnlyWhenTheContextHasOne`, `cli.TestServeInternalPortAndRequestIds` |
| I-65 | Метрики и профили процесса отдаются только внутренним портом и только с секретом, если он задан; на публичном порту их нет; pprof без явного включения отсутствует | `opsserver.TestSecretGuardsMetricsAndProfiles`, `TestProfilesAreAbsentUnlessSwitchedOn`, `TestOnlyDeclaredAddressesAndMethodsExist`, `cli.TestServeInternalPortAndRequestIds` (публичный порт отвечает 404 на все адреса внутреннего) |
| I-66 | Боевой режим не стартует с внутренним портом в чужой сети без секрета, с включённым pprof без секрета, с отчётами об ошибках по http в интернет; ошибка в адресе GlitchTip останавливает запуск и не печатает ключ | `config.TestLoadObservability` (строки «production …» и «bad») |
| I-67 | Проверка готовности отвечает «ok/fail» по каждой проверке без текста ошибки; причина пишется в журнал один раз при смене состояния; при остановке процесс «не готов», но «жив» | `opsserver.TestReadinessReportsChecksWithoutErrorText`, `TestDrainingIsNotReadyButStillAlive`, `TestSlowCheckFailsAtTheDeadline` |
| I-68 | У меток метрик конечное число значений: шаблон маршрута, а не путь; метод вне известных `OTHER`; имена запросов к базе ограничены; посетитель не может раздуть число рядов | `metrics.TestRouteLabelNeverFollowsThePath`, `TestQueryNamesAreBounded`, `TestResponsesWrittenBeforeRoutingAreCountedByTheirRoute` |
| I-69 | В отчёт об ошибке попадают только разрешённые поля после маски; значения неназванных ключей не отправляются вовсе (только имена); текст ошибки базы сведён к коду; пути в стеке обрезаны, аргументов функций нет | `errreport.TestEventGoesToTheProjectWithThePublicKeyAndNothingElse`, `TestDatabaseErrorsAreReducedToTheirCode`, `TestStackParsing`, `TestClipsHugeFields`, `cli.TestServerErrorReachesLogMetricsAndTracker` |
| I-70 | Отправка отчётов не блокирует работу и не заливает сервис: очередь ограничена, повторы сворачиваются, число в минуту ограничено; сбой отправки считается и пишется в журнал редко, не порождая новых отчётов | `errreport.TestSlowTrackerNeverBlocksTheProgram`, `TestOverallLimitDropsInsteadOfFlooding`, `TestRepeatsAreFoldedAndCounted`, `TestDeliveryFailuresAreCountedAndLoggedRarely`, `TestConcurrentCaptureAndClose` |
| I-71 | Сбой источника метрик не роняет опрос и не выдаётся за свежие данные: значения прежние, `scibox_source_up` = 0; опрос не нагружает базу (кеш, один запрос за раз, срок) | `opsmetrics.TestFailingSourceKeepsTheLastValueAndSaysSo`, `TestSourceThatNeverAnswersFailsAtTheDeadlineAndNeverBlocksScrapes` |
| I-72 | Ошибка сервера проходит всю цепочку: ответ 500 без подробностей, запись журнала с номером запроса, отчёт с тем же номером без текста базы, счётчики 500 и записей error | `cli.TestServerErrorReachesLogMetricsAndTracker` |

## Реестр угроз

Статусы: `открыта` · `закрыта (срез, тест)` · `принята (причина, D-NNN)`. Номер новой угрозы следующий по порядку; закрытые не удаляются.

| ID | Угроза | Срез | Статус |
|---|---|---|---|
| T-01 | Уязвимости стандартной библиотеки Go 1.26.5 на путях кода (6 шт., `make vulncheck`) | 18 | закрыта (срез 18: `toolchain go1.26.6`, `make vulncheck` = 0 на путях кода, входит в `make test` и CI) |
| T-02 | 81 замечание линтера в рабочем коде (29 × G115 переполнение при переводе чисел, непроверенные `Close`, `%v` вместо `%w`) | 18 | закрыта (срез 18: `make lint` = 0 при 164 исходных замечаниях, плюс gocritic, revive, nolintlint; входит в `make test` и CI; `TestInt32`, huge offset в `TestHTTPPublicPages`, `TestHTTPErrors`) |
| T-03 | Нет автоматических проверок при отправке в GitHub: уязвимость или красный тест могут попасть в main | 18 | закрыта частично (срез 18: `.github/workflows/ci.yml`, `.github/dependabot.yml` в репозитории; прогон на GitHub — после отправки с разрешения пользователя; правило защиты ветки main включает пользователь в настройках GitHub) |
| T-04 | У HTTP-сервера только `ReadHeaderTimeout`: медленный клиент держит соединение (slowloris), нет `MaxHeaderBytes` | 19 | закрыта (срез 19: `httpguard.NewServer`; `TestSlowHeadersAreCut`, `TestHugeHeadersAreRefused`, `TestSlowTransferGetsItsOwnDeadline`, `TestNewServerSetsEveryTimeout`) |
| T-05 | Нет заголовков защиты у сайта и API: CSP, HSTS, `frame-ancestors` (кликджекинг), `Permissions-Policy` | 19 | закрыта (срез 19: `httpguard.Perimeter`; `TestSecurityHeaders`, `TestPerimeter/security_headers_on_every_kind_of_response` — API, ошибка, страница сайта, файл отклика, PDF; сайт с CSP проверен в браузере) |
| T-06 | За прокси все запросы придут с одного IP: лимиты по IP начнут блокировать всех сразу | 19 | закрыта (срез 19: `SCIBOX_TRUSTED_PROXIES`; `TestParseProxies`, `TestClientAddr`, `TestThrottleBehindProxy`, `TestPerimeter/client_address_behind_a_trusted_proxy_reaches_sessions`) |
| T-07 | Публичные и тяжёлые адреса без ограничения частоты: `/api/landing`, `/api/scientists`, поиск вакансий, `/api/recommendations/*`, `/api/journals`, поиск по DOI | 19 | закрыта (срез 19: `httpguard.Throttle`, `httpapi.PerimeterRoutes`, счётчики в `rate_counters`; `TestThrottle`, `TestThrottleConcurrent`, `TestThrottleStoreFailure`, `TestPostgresStore`, `TestPerimeter/public_reads_are_rate_limited_in_the_database`) |
| T-08 | Сервер стартует с опасными настройками в бою: http-адрес сайта, пароль базы по умолчанию, `sslmode=disable` к удалённой базе | 19 | закрыта (срез 19: `SCIBOX_ENV=production`, `config.checkProduction`; `TestProductionRefusesUnsafeSettings`) |
| T-09 | Не проверено, что каждый адрес ограничивает размер тела | 19 | закрыта (срез 19: `httpguard.Perimeter`; `TestBodyLimits`, `TestPerimeter/every_route_limits_the_request_body` — все адреса настоящей сборки, гостем и вошедшим) |
| T-10 | Нет списка сессий и «выйти везде»; смена и сброс пароля не завершают другие сессии (проверить) | 20 | закрыта (срез 20: список сеансов и завершение любого, вход завершает прежний сеанс браузера; `TestListSessions`, `TestRevokeSession`, `TestHTTPSessions`, `TestHTTPLoginReplacesTheBrowserSession`, e2e `account-security.spec.ts`) |
| T-11 | Нельзя сменить почту и удалить аккаунт (право по 152-ФЗ) | 20 | закрыта (срез 20, D-142, D-143, D-145: `TestEmailChangeFlow`, `TestRevertEmailChange`, `TestDeletionLifecycle`, `TestEraseRemovesPersonalData`, `TestEveryReferenceToTheAccountIsHandled`) |
| T-12 | Опасные действия (смена почты, удаление, передача владения) без повторного ввода пароля | 20 | закрыта (срез 20, D-144: I-25) |
| T-13 | Нет письма о входе с нового устройства и о смене пароля | 20 | закрыта (срез 20, D-146: `TestLoginFromNewDeviceSendsMail`, `TestDeviceTokenFromAnotherPersonOrGarbage`; письмо о смене пароля было со среза 3) |
| T-14 | Простые пароли из утёкших списков принимаются | 20 | закрыта (срез 20, D-146: I-29) |
| T-15 | Нет автоматической проверки, что у каждого адреса API есть строка в таблице прав; новый адрес может появиться без проверки доступа | 21 | закрыта (срез 21, D-149: `cli.TestAccessTable`, I-30; новый адрес без строки роняет `make test`) |
| T-16 | На чужой объект часть адресов может отвечать 403 вместо 404 и подтверждать его существование (проверить все) | 21 | закрыта (срез 21, D-149: проверены все 103 адреса, найден и исправлен приём приглашения по номеру; I-31) |
| T-17 | Нет журнала действий: не восстановить, кто сменил роль, статус отклика, кто скачал файл | 21 | закрыта (срез 21, D-147, D-148: `internal/audit`, I-32…I-34) |
| T-18 | Почты, IP и токены могут попадать в логи; нет правила маскирования | 22 | закрыта (срез 22, D-152: `internal/logmask`, I-37, I-38) |
| T-19 | Нет сроков хранения: старые сессии, попытки, токены, тела писем в очереди, уведомления лежат вечно | 22 | закрыта (срез 22, D-151, D-152: `internal/retention`, I-39…I-41; сессии, ссылки, счётчики, знакомые браузеры были закрыты раньше) |
| T-20 | Человек не может выгрузить свои данные (право на доступ, 152-ФЗ) | 22 | закрыта (срез 22, D-153: `internal/dataexport`, I-43) |
| T-21 | Файлы откликов лежат в базе без проверки на вирусы (D-080); большие файлы читаются в память целиком | 23 | закрыта (срез 23, D-157…D-160: хранилище S3, ClamAV, загрузка потоком; I-44…I-51) |
| T-22 | Тяжёлый запрос к базе может работать бесконечно: нет `statement_timeout`, `idle_in_transaction_session_timeout` | 24 | закрыта (срез 24, D-164: `internal/dbpool`, I-52…I-54; запрос, остановленный по сроку, даёт 503 `database_busy`) |
| T-23 | При двух копиях сервера фоновые задачи подбора и напоминаний могут выполниться дважды (проверить блокировки) | 25 | закрыта (срез 25, D-167, D-170: общее расписание `job_runs`, `jobs`; тест двух серверов и двух воркеров на 1000 событий I-59; I-57, I-58) |
| T-24 | Зависший Crossref или SMTP держит запросы и горутины | 25 | закрыта (срез 25, D-168, D-169: сроки, ограничитель одновременных, предохранитель, виды сбоя письма; I-60, I-61) |
| T-25 | `pprof` и метрики, если появятся, не должны быть доступны из интернета | 26 | закрыта (срез 26, D-173: отдельный внутренний порт с секретом, I-65, I-66; тест на публичном порту) |
| T-26 | Нет резервных копий и проверенного восстановления | 28 | открыта |
| T-27 | Образ для выкладки ещё не существует: нужен без root, с файловой системой только для чтения, без лишних программ | 28 | открыта |
| T-28 | Не было полного аудита: статический анализ (semgrep), фаззинг разборщиков, DAST (OWASP ZAP), ASVS L2 | 29 | открыта |
| T-30 | Множество сетей IPv6 /64 у одного злоумышленника создают много строк `rate_counters` (по строке на сеть в минуту, живут 2 часа) | 27 | открыта (замерить рост таблицы под нагрузкой; при необходимости считать гостей IPv6 по /56) |
| T-31 | Доверенный прокси, который не дописывает адрес в X-Forwarded-For, а передаёт заголовок посетителя как есть, позволяет подделать адрес. Для tuna и localhost.run (`make share`) поведение не документировано | 28 | открыта (при выборе хостинга проверить прокси на живом запросе; для показа по ссылке последствие — обход своего лимита) |
| T-32 | Ссылка «это не я» (7 дней) даёт тому, кто читает старую почту, вернуть её себе и получить ссылку для пароля. Плохо, если почту меняли именно из-за взломанного старого ящика | 29 | принята (D-145: защита от вора сеанса важнее, у владельца старого ящика и раньше был сброс пароля; пересмотреть при аудите) |
| T-33 | После стирания аккаунта у сотрудников организаций остаются уведомления об ответах на приглашения на вакансии (в тексте имя) и письма в очереди `outbox` с его данными (до 7 дней) | 22 | закрыта (срез 22: стираются уведомления об ответах на приглашения и история писем рекомендателям, текст писем стирается после отправки; I-41, I-42) |
| T-34 | Гонка: организация одиночки, в которую в момент стирания вошёл сотрудник по старому приглашению, остаётся без владельца | 21 | закрыта (срез 21, D-150: блокировка организации, I-35) |
| T-35 | Журнал растёт без ограничения (запись на каждый вход и каждую выдачу файла) и хранит адреса и скрытые почты приглашённых, пока аккаунт жив | 22 | закрыта (срез 22, D-151: журнал 3 года, адрес 180 дней; I-39) |
| T-36 | В журнале нет повторных открытий карточки отклика и чтения писем рекомендателей в карточке: кто смотрел данные кандидата второй раз, не видно | 29 | принята (D-147: журнал отвечает на вопрос «кто принял решение и скачал файл»; пересмотреть при аудите) |
| T-37 | Отклики (письмо, контакты, снимок профиля, файлы) у организаций хранятся, пока человек сам не удалит аккаунт, даже если вакансия закрыта давно | 22 | принята (D-151: пользователь оставил как есть, вопрос юристу: нужен ли срок и какой; после ответа новый срез чистки) |
| T-38 | Неактивные аккаунты (подтверждённая почта, много лет без входа) не удаляются | 29 | открыта (нужно решение пользователя и юриста о сроке; при аудите) |
| T-39 | Ожидающие письма третьим лицам (организации, рекомендателю), в тексте которых имя удалённого человека, дойдут после удаления: очередь не знает, о ком письмо | 22 | принята (окно не больше суток пока идут повторы; отправленные письма уже без текста) |
| T-40 | Резервные копии (срез 28) будут хранить данные, которые человек уже удалил | 28 | открыта (записать срок хранения копий и порядок: после восстановления повторить удаления) |
| T-41 | Локальный ClamAV облегчённый (одна сигнатура EICAR, без обновлений): боевую настройку (полные базы, `freshclam`, 2 ГБ памяти, размер `StreamMaxLength` не меньше 25 МБ) на этом компьютере не проверить | 28 | открыта (проверить при выкладке: EICAR, настоящая заражённая тестовая сигнатура из базы, обновление баз; docs/DEPLOY.md) |
| T-42 | PDF может содержать активное содержимое (скрипты), которое ClamAV находит не всегда | 23 | принята (D-158: файлы выдаются только как скачивание с `Content-Disposition: attachment`, `nosniff` и `CSP: sandbox`, показ внутри сайта не делаем; пересмотреть при аудите, срез 29) |
| T-43 | Содержимое файлов в хранилище не шифруется нами: шифрование на стороне провайдера и закрытость бакета (без публичного доступа, ключ только у сервера) задаёт администратор | 28 | открыта (записать в docs/DEPLOY.md: бакет приватный, шифрование на стороне провайдера включено, ключ сервисного аккаунта только на запись, чтение и удаление в одном бакете) |
| T-44 | Очередь файлов «на проверке» может расти молча (антивирус лежит днями): пока видно только по журналу | 26 | закрыта (срез 26, D-174: `scibox_files_pending_scan`, `…_oldest_pending_scan_seconds`, `…_blob_deletions_queued`; тревоги `FileScanBacklog`, `BlobDeletionBacklog`, проверены `promtool`; на живом стенде при упавшем ClamAV панель показала 24 файла в очереди) |
| T-45 | Поиск по частому слову и с опечатками читает тексты тысяч строк (в замере до ~100 мс на запрос); при 300 запросах в минуту с одного адреса (лимит `public_read`) это заметная нагрузка на базу | 27 | открыта (замерить под нагрузкой, срез 27; при необходимости снизить лимит для поиска по словам или кешировать частые запросы) |
| T-46 | Параметры запуска соединения (`statement_timeout`, `plan_cache_mode`, `random_page_cost`) не работают через пул соединений в режиме transaction (PgBouncer) | 28 | открыта (при выборе хостинга: если пул соединений нужен, задать их на роль базы `ALTER ROLE … SET …`; проверить `SHOW statement_timeout` на живом соединении) |
| T-47 | Миграция 00021 выполняет `ALTER DATABASE … SET pg_trgm.word_similarity_threshold`; на управляемом хостинге у пользователя приложения может не быть такого права, и поиск с опечатками тогда работал бы на пороге 0,6, находя меньше | 28 | открыта (проверить на выбранном хостинге; запасной путь: задать значение в параметрах роли или `postgresql.conf`) |
| T-48 | Письмо может уйти дважды, если процесс упал между ответом почтового сервера «принято» и записью об этом в базу (у SMTP нет идемпотентности) | 25 | принята (D-168: окно в доли секунды на письмо; повтор безвреден для писем со ссылками, у которых новая ссылка отменяет прежнюю; пересмотреть при выборе почтового сервиса: у многих есть ключ идемпотентности) |
| T-49 | У воркера нет HTTP: проверить, что он жив и не стоит, можно только по журналу; сбой задачи виден в журнале и в таблице `job_runs`, но не в метриках | 26 | закрыта (срез 26, D-173, D-174: внутренний порт воркера, метрики задач, тревоги `WorkerDown`, `JobNotRunning`, `JobFailing`, `LocalJobNotRunning`, `JobsPausedByClock`; на живом стенде при остановленной базе воркер и сервер сами восстановились без перезапуска, тревоги погасли) |
| T-50 | Предохранители Crossref и почты у каждой копии свои: при N копиях Crossref получит до N·5 лишних обращений, пока все не откроются, а общий предел одновременных (1 на публичном пуле) равен N | 27 | открыта (мало копий — мало вреда; при росте числа копий хранить состояние предохранителя в базе или поставить общий лимит; замерить при нагрузочном тесте) |
| T-51 | В отчёт об ошибке может попасть свободный текст ошибки (например, из `fmt.Errorf` со значением из запроса): маска знает почты, адреса и токены, но не имена | 26 | принята (D-172: список разрешённых полей, текст базы сведён к коду, срок хранения 30 дней, стирание IP; правило кода «значения людей в тексты ошибок не класть»; проверить при аудите, срез 29: поиск `Errorf` со значениями) |
| T-52 | Prometheus, Grafana и GlitchTip в бою без входа и без TLS открыли бы устройство системы и тексты ошибок | 28 | открыта (docs/DEPLOY.md: за обратным прокси с входом, порты не публикуются; в разработке вход выключен намеренно, порты на 127.0.0.1) |
| T-53 | Тревоги Prometheus ни к кому не приходят: видны только в панели и на странице тревог | 28 | открыта (выбрать с хостингом: Alertmanager с получателем в Telegram или почте; правила готовы и проверены) |
| T-29 | `golang.org/x/crypto/openpgp` (GO-2026-5932) в модуле x/crypto: пакет не поддерживается, исправления нет; наш код его не вызывает (govulncheck: «модули», не «код») | 29 | принята (код не импортирует openpgp, исправления нет; проверить при аудите, D-140) |

## Карта персональных данных

Составлена в срезе 22 (T-18…T-20, D-151, D-152). Каждая таблица базы имеет строку: какие поля содержат персональные данные (ПДн, 152-ФЗ), чьи они, кто их видит, сколько хранятся и чем удаляются. Тест `erasure.TestPersonalDataMapCoversEveryTable` сверяет первый столбец с настоящими таблицами базы: новая таблица без строки здесь роняет `make test`. Столбец «Удаление при удалении аккаунта» проверяет `cli.TestErasedPersonLeavesNothingToRecogniseThemBy`: после стирания все таблицы просматриваются целиком, и почты, имени, идентификаторов, адреса входа и почт рекомендателей человека нигде нет.

Сроки: «аккаунт» — пока жив аккаунт человека; числа — решения D-151 (пользователь) и D-152 (Claude). Чистка по сроку: `internal/retention` (раз в час), `auth.Cleanup`, `orgs.Cleanup`, `notifications.Worker.Cleanup`, `httpguard.PostgresStore.RunCleanup`.

| Таблица | Поля с ПДн | Чьи | Кто видит | Срок хранения | Удаление при удалении аккаунта |
|---|---|---|---|---|---|
| `users` | почта, имя, хеш пароля, дата согласия | человек | сам; имя видят организации (в отклике, в списке сотрудников), почту видит владелец организации у сотрудников | аккаунт; регистрация без подтверждённой почты 7 дней после последней активности | остаётся строка-«надгробие» без почты, имени и пароля (`erasure.TombstoneUser`); неподтверждённые стирает `retention` |
| `sessions` | адрес (IP), браузер (User-Agent) | человек | сам (браузер и время; адрес не показывается) | до 30 дней, затем сеанс удаляется | стираются |
| `auth_tokens` | новая почта (при смене), хеш ссылки | человек | никто | до 24 часов после срока ссылки (сроки 1 час…7 дней) | стираются |
| `known_devices` | хеш cookie браузера | человек | никто | 400 дней без входа | стираются |
| `rate_events` | ключ счётчика: почта, адрес (IP) или номер аккаунта | человек, гость | никто | 24 часа | счётчики по почте и номеру аккаунта стираются; по адресу истекают сами |
| `rate_counters` | ключ: сеть гостя (IPv4 адрес, IPv6 /64) или номер аккаунта | человек, гость | никто | 2 часа после окна | не стираются: истекают сами |
| `profiles` | должность, город, о себе, степень и звание, ORCID, SPIN, Scopus, WoS, h-индексы, контактная почта, навыки | человек | по режиму приватности (D-072): скрыт, организациям, публичный; контакты только сотрудникам организаций | аккаунт | стирается |
| `profile_items` | образование, места работы, публикации, гранты, патенты, преподавание (JSON) | человек | как профиль | аккаунт | стираются вместе с профилем |
| `profile_specialties` | области науки профиля | человек | как профиль | аккаунт | стираются вместе с профилем |
| `applications` | письмо, контактная почта, снимок профиля, записка к решению | соискатель | сам; сотрудники с правом на отклики этой вакансии | аккаунт; вопрос о сроке после закрытия вакансии передан юристу (T-37) | обезличиваются: остаются вакансия, даты и статус (D-142) |
| `application_files` | имя; содержимое резюме, приложений и писем рекомендателей лежит не в базе, а в S3-хранилище (в базе ключ объекта, контрольная сумма, итог проверки на вирусы) | соискатель, рекомендатель | соискатель (кроме писем рекомендателей); сотрудники; файл выдаётся только после проверки на вирусы | как отклик | строки стираются, объекты ставятся в очередь `blob_deletions` в той же транзакции и удаляются из хранилища фоновой задачей `filework` |
| `blob_deletions` | нет ПДн (случайные ключи объектов, номер человека в них не входит) | — | — | до удаления объекта из хранилища | — |
| `application_invitations` | контакты сотрудника организации, контакты и удобное время соискателя | соискатель, сотрудник | обе стороны отклика | как отклик | стираются |
| `reference_requests` | имя, почта и письмо рекомендателя, связь с соискателем | рекомендатель | организация (письмо), соискатель (только состояние просьбы) | как отклик; ссылка живёт 30 дней | стираются; история отправленных им писем тоже (`EraseRefereeMailHistory`) |
| `vacancy_offers` | сообщение организации, ответ соискателя | соискатель | обе стороны | аккаунт | стираются |
| `favorites` | выбор вакансий | человек | сам | аккаунт | стираются |
| `saved_searches` | условия и названия поисков | человек | сам | аккаунт | стираются |
| `deadline_reminders` | какие напоминания отправлены | человек | никто | аккаунт | стираются |
| `notification_settings` | выбор писем | человек | сам | аккаунт | стираются |
| `notifications` | заголовок и текст (имена кандидатов у сотрудников, тексты решений) | получатель; кандидат в уведомлениях сотрудников | получатель | прочитанные 90 дней, непрочитанные год (D-151) | свои стираются; уведомления сотрудников об откликах и ответах на приглашения, где в тексте имя человека, стираются по ссылке на отклик или приглашение (`EraseCandidateNotifications`, `EraseOfferAnswerNotifications`) |
| `outbox` | адрес получателя, тема и текст письма, текст последней ошибки отправки | получатель; в тексте могут быть чужие имена | никто | текст стирается сразу после отправки или окончательной неудачи; строка с адресом 7 дней (отправленное) и 30 дней (неудавшееся) | письма на почту человека и отправленные его рекомендателям стираются; ожидающие письма третьим лицам (до ~1 суток, пока идут повторы) доставляются |
| `org_members` | номер аккаунта, роль | сотрудник | владелец организации (с почтой и именем) | аккаунт | стираются |
| `org_invitations` | почта приглашённого | приглашённый | владелец организации | неделя ссылки и ещё 30 дней | приглашения на почту человека стираются |
| `organizations` | ссылка на создателя (`created_by`); название, сайт и описание — данные организации, не человека | сотрудник | публично | пока существует организация | ссылка остаётся на строке-«надгробии» |
| `units` | ссылка на руководителя (`head_user_id`) | руководитель | публично (имя), владелец (номер) | пока существует подразделение | руководитель снимается |
| `vacancies` | ссылка на создателя (`created_by`); текст вакансии — данные организации | сотрудник | публично | пока существует вакансия | ссылка остаётся на строке-«надгробии» |
| `audit_log` | номера аккаунтов, адрес (IP), подпись объекта, скрытая почта приглашённого | человек, сотрудник | владелец организации (организационные записи, без адреса); личные записи входят в архив выгрузки своих данных | 3 года (D-151); адрес 180 дней (D-152) | адрес стирается; записи остаются (история организации), имя берётся из аккаунта и показывается «Аккаунт удалён» |
| `job_runs` | нет ПДн (имена задач, времена, текст последней ошибки без данных людей: адреса, почты и токены в ошибки не попадают, журнал проходит через маску) | — | — | — | — |
| `vacancy_specialties` | нет ПДн | — | — | — | — |
| `profile_search` | готовый текст профиля для поиска: имя, должность, город, регион, организация степени, специальности, навыки, «о себе»; коды специальностей | человек | только каталог учёных, по режимам приватности профиля (сам текст наружу не отдаётся) | как профиль (срез 24, D-162) | стирается вместе с профилем (`ON DELETE CASCADE`) |
| `vacancy_search` | нет ПДн (текст вакансии) | — | — | — | — |
| `journals` | нет ПДн (справочник SCImago) | — | — | — | — |
| `journal_issns` | нет ПДн | — | — | — | — |
| `positions` | нет ПДн (справочник) | — | — | — | — |
| `regions` | нет ПДн (справочник) | — | — | — | — |
| `science_fields` | нет ПДн (справочник) | — | — | — | — |
| `science_groups` | нет ПДн (справочник) | — | — | — | — |
| `specialties` | нет ПДн (справочник) | — | — | — | — |
| `reference_sources` | нет ПДн (источники справочников) | — | — | — | — |

Где ПДн нет или почти нет:
- Журнал сервера (stderr): почты, адреса (IPv4 до /24, IPv6 до /48), токены и пароли маскируются слоем `internal/logmask` (I-37, I-38); имена и тексты людей в журнал не пишутся по правилу кода.
- GlitchTip (срез 26, D-172): отчёты об ошибках содержат очищенное сообщение, шаблон адреса, номер запроса, имена задач и запросов, коды; без почт, имён, адресов посетителей, тел запросов и cookie; хранятся 30 дней; у организации и проекта включено стирание IP.
- Метрики Prometheus (срез 26): только числа и конечные метки (шаблоны адресов, имена задач и запросов); данных людей нет.
- Резервные копии появятся в срезе 28: срок их хранения и порядок восстановления после удаления аккаунта записать там.
- Не хранится совсем: пароли открытым текстом (только argon2id), токены (только SHA-256), письма рекомендателей соискателю не отдаются.
- Известные остатки после удаления аккаунта: ожидающие письма третьим лицам (до ~1 суток); счётчики по адресу (до 24 часов); обезличенные отклики и записи «кто сделал» (нужны организации, D-142).
