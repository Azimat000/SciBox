#!/usr/bin/env python3
"""Собирает панель Grafana «ИОН: сервер» в dashboards/scibox.json (срез 26).

Панель описана здесь, а не руками в JSON: так видно, какой запрос стоит за каждым элементом, и правка одного места
не ломает остальные. После правки: `python3 monitoring/grafana/build-dashboard.py`, затем перезапустить Grafana
(`docker compose -f docker-compose.monitoring.yml restart grafana`: читает файл при старте).
"""
import json
import os
DS={"type":"prometheus","uid":"prometheus"}
pid=[0]
def nid():
    pid[0]+=1; return pid[0]
def target(expr, legend="", ref="A", instant=False):
    t={"datasource":DS,"expr":expr,"legendFormat":legend,"refId":ref}
    if instant: t["instant"]=True; t["range"]=False
    return t
def row(title,y):
    return {"type":"row","title":title,"collapsed":False,"gridPos":{"h":1,"w":24,"x":0,"y":y},"id":nid(),"panels":[]}
def stat(title,expr,x,y,w=4,h=4,unit="short",thresholds=None,decimals=None,desc="",mappings=None):
    steps=thresholds or [{"color":"green","value":None}]
    return {"type":"stat","title":title,"description":desc,"id":nid(),"datasource":DS,"gridPos":{"h":h,"w":w,"x":x,"y":y},
      "targets":[target(expr,instant=True)],
      "fieldConfig":{"defaults":{"unit":unit,"decimals":decimals,"thresholds":{"mode":"absolute","steps":steps},"mappings":mappings or [],"noValue":"нет данных"},"overrides":[]},
      "options":{"reduceOptions":{"calcs":["lastNotNull"],"fields":"","values":False},"colorMode":"background","graphMode":"none","textMode":"auto"}}
def ts(title,exprs,x,y,w=8,h=8,unit="short",desc="",stack=False,min_=None,max_=None):
    ts_=[target(e,l,chr(65+i)) for i,(e,l) in enumerate(exprs)]
    d={"unit":unit,"custom":{"lineWidth":2,"fillOpacity":10 if not stack else 40,"showPoints":"never","stacking":{"mode":"normal" if stack else "none"}}}
    if min_ is not None: d["min"]=min_
    if max_ is not None: d["max"]=max_
    return {"type":"timeseries","title":title,"description":desc,"id":nid(),"datasource":DS,"gridPos":{"h":h,"w":w,"x":x,"y":y},
      "targets":ts_,"fieldConfig":{"defaults":d,"overrides":[]},
      "options":{"legend":{"displayMode":"list","placement":"bottom","showLegend":True},"tooltip":{"mode":"multi","sort":"desc"}}}
def table(title,expr,x,y,w=12,h=8,desc="",transform=None):
    p={"type":"table","title":title,"description":desc,"id":nid(),"datasource":DS,"gridPos":{"h":h,"w":w,"x":x,"y":y},
      "targets":[dict(target(expr,instant=True),format="table")],"fieldConfig":{"defaults":{},"overrides":[]},"options":{"showHeader":True},
      "transformations":transform or []}
    return p

P=[]
y=0
P.append(row("Главное",y)); y+=1
red_at=lambda v:[{"color":"green","value":None},{"color":"red","value":v}]
yel=lambda a,b:[{"color":"green","value":None},{"color":"yellow","value":a},{"color":"red","value":b}]
P.append(stat("Тревоги",'count(ALERTS{alertstate="firing",severity=~"warning|critical"}) or vector(0)',0,y,4,4,thresholds=[{"color":"green","value":None},{"color":"red","value":1}],decimals=0,desc="Тревоги: сколько горит сейчас. Подробности в таблице ниже и на http://localhost:9190/alerts"))
P.append(stat("Запросы/с",'sum(rate(scibox_http_requests_total[5m]))',4,y,4,4,unit="reqps",decimals=1,desc="Все запросы к серверу за 5 минут. Цель пиковой нагрузки: 500."))
P.append(stat("Ошибки 5xx",'(sum(rate(scibox_http_requests_total{status=~"5.."}[5m])) / sum(rate(scibox_http_requests_total[5m]))) or vector(0)',8,y,4,4,unit="percentunit",decimals=2,thresholds=yel(0.001,0.01),desc="Доля ответов «сервер сломался». Цель: меньше 0,1%."))
P.append(stat("Ответ p95",'histogram_quantile(0.95, sum by (le) (rate(scibox_http_request_duration_seconds_bucket[5m])))',12,y,4,4,unit="s",decimals=3,thresholds=yel(0.2,0.5),desc="95% запросов быстрее этого времени. Цель: 0,2 с для чтения."))
P.append(stat("Письма",'max(scibox_mail_queue_messages{state="waiting"})',16,y,4,4,decimals=0,thresholds=yel(100,1000),desc="Письма в очереди на отправку."))
P.append(stat("Файлы",'max(scibox_files_pending_scan)',20,y,4,4,decimals=0,thresholds=yel(10,100),desc="Файлы откликов, которые ещё не проверил антивирус и поэтому недоступны организациям."))
y+=4
P.append(stat("Сервер",'max(up{job="scibox-serve"})',0,y,3,3,mappings=[{"type":"value","options":{"0":{"text":"НЕ ОТВЕЧАЕТ","color":"red"},"1":{"text":"работает","color":"green"}}}],thresholds=[{"color":"red","value":None},{"color":"green","value":1}]))
P.append(stat("Воркер",'max(up{job="scibox-worker"})',3,y,3,3,mappings=[{"type":"value","options":{"0":{"text":"НЕ ОТВЕЧАЕТ","color":"red"},"1":{"text":"работает","color":"green"}}}],thresholds=[{"color":"red","value":None},{"color":"green","value":1}]))
P.append(stat("Старое письмо",'max(scibox_mail_queue_oldest_waiting_seconds)',6,y,4,3,unit="s",thresholds=yel(300,900)))
P.append(stat("Недоставлено",'max(scibox_mail_queue_messages{state="undelivered"})',10,y,4,3,decimals=0,thresholds=yel(1,10)))
P.append(stat("Пул базы",'max(scibox_db_pool_connections{state="acquired"} / ignoring (state) scibox_db_pool_max_connections)',14,y,5,3,unit="percentunit",decimals=0,thresholds=yel(0.7,0.9)))
P.append(stat("Диск свободен",'min(node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"})',19,y,5,3,unit="percentunit",decimals=0,thresholds=[{"color":"red","value":None},{"color":"yellow","value":0.05},{"color":"green","value":0.15}]))
y+=3
P.append(table("Горящие тревоги",'ALERTS{alertstate="firing",alertname!="Watchdog"}',0,y,24,6,desc="Что горит сейчас. Пусто — всё в порядке.",
  transform=[{"id":"organize","options":{"excludeByName":{"Time":True,"Value":True,"__name__":True,"alertstate":True,"job":True,"instance":True},"indexByName":{},"renameByName":{"alertname":"Тревога","severity":"Серьёзность","service":"Процесс","task":"Задача","name":"Имя"}}}]))
y+=6

P.append(row("Запросы",y)); y+=1
P.append(ts("Запросы в секунду по адресам (первые 8)",[('topk(8, sum by (route) (rate(scibox_http_requests_total[5m])))',"{{route}}")],0,y,12,8,unit="reqps",desc="Шаблон адреса, а не путь: номера вакансий и людей не видны."))
P.append(ts("Ответы по коду",[('sum by (status) (rate(scibox_http_requests_total[5m]))',"{{status}}")],12,y,12,8,unit="reqps",stack=True,desc="429 — сработало ограничение частоты; 5xx — ошибки сервера."))
y+=8
P.append(ts("Время ответа",[('histogram_quantile(0.50, sum by (le) (rate(scibox_http_request_duration_seconds_bucket[5m])))',"p50"),('histogram_quantile(0.95, sum by (le) (rate(scibox_http_request_duration_seconds_bucket[5m])))',"p95"),('histogram_quantile(0.99, sum by (le) (rate(scibox_http_request_duration_seconds_bucket[5m])))',"p99")],0,y,12,8,unit="s",desc="Цели: p95 ≤ 0,2 с, p99 ≤ 1 с."))
P.append(ts("Самые медленные адреса по p95 (первые 8)",[('topk(8, histogram_quantile(0.95, sum by (le, route) (rate(scibox_http_request_duration_seconds_bucket[5m]))))',"{{route}}")],12,y,12,8,unit="s"))
y+=8
P.append(ts("Ошибки 5xx по адресам",[('sum by (route) (rate(scibox_http_requests_total{status=~"5.."}[5m]))',"{{route}}")],0,y,8,7,unit="reqps"))
P.append(ts("Ограничение частоты (429) по адресам",[('sum by (route) (rate(scibox_http_requests_total{status="429"}[5m]))',"{{route}}")],8,y,8,7,unit="reqps"))
P.append(ts("Запросы в обработке",[('sum by (service) (scibox_http_requests_in_flight)',"{{service}}")],16,y,8,7))
y+=7

P.append(row("База данных",y)); y+=1
P.append(ts("Соединения пула",[('scibox_db_pool_connections',"{{service}} {{state}}"),('scibox_db_pool_max_connections',"предел {{service}}")],0,y,8,8,desc="acquired — заняты запросами; idle — свободны."))
P.append(ts("Соединения со всей базой",[('max(scibox_db_server_connections)',"занято всеми процессами"),('max(scibox_db_server_max_connections)',"предел базы")],8,y,8,8))
P.append(ts("Ожидание соединения из пула",[('sum by (service) (rate(scibox_db_pool_empty_acquires_total[5m]))',"{{service}}: раз в секунду")],16,y,8,8,unit="ops",desc="Когда не ноль, запросам не хватает соединений."))
y+=8
P.append(ts("Время запросов к базе",[('histogram_quantile(0.50, sum by (le) (rate(scibox_db_query_duration_seconds_bucket[5m])))',"p50"),('histogram_quantile(0.95, sum by (le) (rate(scibox_db_query_duration_seconds_bucket[5m])))',"p95"),('histogram_quantile(0.99, sum by (le) (rate(scibox_db_query_duration_seconds_bucket[5m])))',"p99")],0,y,8,8,unit="s"))
P.append(ts("Медленные запросы и остановленные по сроку",[('sum by (query) (rate(scibox_db_slow_queries_total[5m]))',"медленный {{query}}"),('sum by (query) (rate(scibox_db_query_timeouts_total[5m]))',"по сроку {{query}}")],8,y,8,8,unit="ops"))
P.append(ts("Размер базы",[('max(scibox_db_size_bytes)',"размер")],16,y,8,8,unit="bytes"))
y+=8

P.append(row("Письма, файлы, внешние сервисы",y)); y+=1
P.append(ts("Очередь писем",[('max by (state) (scibox_mail_queue_messages)',"{{state}}")],0,y,8,8,desc="waiting — ждут (включая повторяемые), retrying — из них пробовали, undelivered — окончательно не доставлены."))
P.append(ts("Возраст самого старого письма",[('max(scibox_mail_queue_oldest_waiting_seconds)',"ждёт")],8,y,8,8,unit="s"))
P.append(ts("Предохранители и ограничители",[('max by (name) (scibox_breaker_state)',"предохранитель {{name}} (0 закрыт, 1 открыт, 2 проба)"),('max by (name) (scibox_limiter_in_flight)',"в работе {{name}}")],16,y,8,8))
y+=8
P.append(ts("Очередь проверки файлов на вирусы",[('max(scibox_files_pending_scan)',"ждут проверки"),('max(scibox_files_blob_deletions_queued)',"ждут удаления из хранилища"),('max(scibox_files_legacy_in_database)',"ещё в базе")],0,y,12,8))
P.append(ts("Как давно ждёт самый старый файл",[('max(scibox_files_oldest_pending_scan_seconds)',"ждёт")],12,y,12,8,unit="s",desc="Растёт, если антивирус недоступен."))
y+=8

P.append(row("Чат: живой поток и письма",y)); y+=1
P.append(stat("Потоки открыто",'sum(scibox_chat_streams_open{job="scibox-serve"})',0,y,4,4,decimals=0,desc="Живые потоки чата на всех копиях. Цель среза 39: 5000 на две копии."))
P.append(stat("Доставка p95",'histogram_quantile(0.95, sum by (le) (rate(scibox_chat_delivery_seconds_bucket[5m])))',4,y,4,4,unit="s",decimals=3,thresholds=yel(1,2),desc="От записи события до записи в поток собеседника. Цель: не больше 2 секунд."))
P.append(stat("Слушатель",'min(scibox_chat_listener_up{job="scibox-serve"})',8,y,4,4,mappings=[{"type":"value","options":{"0":{"text":"ОБОРВАН","color":"red"},"1":{"text":"работает","color":"green"}}}],thresholds=[{"color":"red","value":None},{"color":"green","value":1}],desc="Соединение LISTEN каждой копии с базой."))
P.append(stat("Писем о сообщениях ждёт",'max(scibox_chat_unread_mail_waiting)',12,y,4,4,decimals=0,desc="Переписки, по которым ещё не ушло письмо о непрочитанном (включая тех, кому ещё не пора)."))
P.append(stat("Старейшее ждёт",'max(scibox_chat_unread_mail_oldest_waiting_seconds)',16,y,4,4,unit="s",thresholds=yel(900,1800),desc="Письмо уходит через 15 минут; заметно больше значит, что задача отстаёт."))
P.append(stat("Отказов в потоке",'sum(increase(scibox_chat_streams_rejected_total[10m]))',20,y,4,4,decimals=0,thresholds=yel(1,50),desc="За 10 минут: предел потоков на человека или на копию."))
y+=4
P.append(ts("Открытые потоки по копиям",[('sum by (instance) (scibox_chat_streams_open{job="scibox-serve"})',"{{instance}}")],0,y,8,8,desc="Предел на копию 3000 (SCIBOX_CHAT_STREAMS)."))
P.append(ts("Задержка доставки",[('histogram_quantile(0.50, sum by (le) (rate(scibox_chat_delivery_seconds_bucket[5m])))',"p50"),('histogram_quantile(0.95, sum by (le) (rate(scibox_chat_delivery_seconds_bucket[5m])))',"p95"),('histogram_quantile(0.99, sum by (le) (rate(scibox_chat_delivery_seconds_bucket[5m])))',"p99")],8,y,8,8,unit="s",desc="От записи события в журнал до записи в поток получателя."))
P.append(ts("События в секунду по виду",[('sum by (kind, stage) (rate(scibox_chat_events_total[5m]))',"{{kind}} {{stage}}")],16,y,8,8,unit="ops",desc="dispatched — обработано копией, delivered — записано в поток получателя."))
y+=8
P.append(ts("Закрытия потоков по причине",[('sum by (reason) (rate(scibox_chat_streams_closed_total[5m]))',"{{reason}}")],0,y,8,7,unit="ops",desc="client — ушёл, age — срок жизни (раз в 4 минуты), slow — не успевает читать, rights/session — потеря прав или конец сессии."))
P.append(ts("Отказы в потоке",[('sum by (reason) (rate(scibox_chat_streams_rejected_total[5m]))',"{{reason}}")],8,y,8,7,unit="ops"))
P.append(ts("Добор пропущенного",[('sum(rate(scibox_chat_catchup_events_total[5m]))',"добрано из журнала")],16,y,8,7,unit="ops",desc="Не ноль, когда уведомление не пришло: обрыв соединения LISTEN или страховочный опрос."))
y+=7

P.append(row("Фоновые задачи",y)); y+=1
P.append(table("Когда задача последний раз завершилась успешно",'time() - max by (task) (scibox_job_last_success_timestamp_seconds)',0,y,12,9,desc="Секунд с последнего успеха по общему расписанию. Больше трёх сроков — тревога.",
  transform=[{"id":"organize","options":{"excludeByName":{"Time":True,"__name__":True},"renameByName":{"task":"Задача","Value":"Секунд назад"}}}]))
P.append(ts("Проходы задач по итогу",[('sum by (task, result) (increase(scibox_job_runs_total[10m]))',"{{task}} {{result}}")],12,y,12,9,desc="Проходы за 10 минут: ok, failed, interrupted."))
y+=9
P.append(ts("Задачи, которые падают подряд",[('max by (task) (scibox_job_consecutive_failures)',"{{task}}")],0,y,12,7))
P.append(ts("Время прохода задач",[('histogram_quantile(0.95, sum by (le, task) (rate(scibox_job_duration_seconds_bucket[15m])))',"{{task}}")],12,y,12,7,unit="s"))
y+=7

P.append(row("Ошибки и журнал",y)); y+=1
P.append(ts("Записи журнала по уровню",[('sum by (level) (rate(scibox_log_events_total[5m]))',"{{level}}")],0,y,8,7,unit="ops"))
P.append(ts("Паники",[('sum by (where) (increase(scibox_panics_total[10m]))',"{{where}}")],8,y,8,7,desc="Любая паника — ошибка в коде."))
P.append(ts("Отчёты об ошибках (GlitchTip)",[('sum by (result) (rate(scibox_error_reports_total[5m]))',"{{result}}")],16,y,8,7,unit="ops",desc="sent — отправлено; suppressed — повторы свёрнуты; dropped — не поместилось; failed — не дошло."))
y+=7

P.append(row("Компьютер и среда выполнения",y)); y+=1
P.append(ts("Диск: свободно",[('node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"}',"свободно")],0,y,8,7,unit="percentunit",min_=0,max_=1))
P.append(ts("Память: свободно",[('node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes',"доступно")],8,y,8,7,unit="percentunit",min_=0,max_=1))
P.append(ts("Процессор (загрузка всех ядер)",[('1 - avg(rate(node_cpu_seconds_total{mode="idle"}[5m]))',"занято")],16,y,8,7,unit="percentunit",min_=0,max_=1))
y+=7
P.append(ts("Горутины",[('go_goroutines{job=~"scibox-.*"}',"{{service}}")],0,y,8,7))
P.append(ts("Память процесса",[('process_resident_memory_bytes{job=~"scibox-.*"}',"{{service}} занято"),('go_memstats_heap_inuse_bytes{job=~"scibox-.*"}',"{{service}} куча")],8,y,8,7,unit="bytes"))
P.append(ts("Сборка мусора (пауза)",[('rate(go_gc_duration_seconds_sum{job=~"scibox-.*"}[5m])',"{{service}}")],16,y,8,7,unit="s"))

dash={"uid":"scibox-server","title":"ИОН: сервер","tags":["scibox"],"timezone":"browser","schemaVersion":39,"version":1,
 "editable":False,"graphTooltip":1,"refresh":"15s","time":{"from":"now-1h","to":"now"},
 "annotations":{"list":[]},"templating":{"list":[]},"links":[],"panels":P,
 "description":"Состояние сервера и воркера: запросы, ошибки, база, очереди, фоновые задачи. Источник: Prometheus (срез 26)."}
out = os.path.join(os.path.dirname(os.path.abspath(__file__)), "dashboards", "scibox.json")
with open(out, "w", encoding="utf-8") as f:
    json.dump(dash, f, ensure_ascii=False, indent=1)
    f.write("\n")
print(len(P),"panels")
