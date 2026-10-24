"""Redis Stream router and isolated fast/slow execution pools."""

from __future__ import annotations

import json
import logging
import os
import signal
import socket
import threading
from concurrent.futures import ThreadPoolExecutor
from typing import Any

import redis

from worker.router import dispatch, lane_for
from worker.backend_protocol import BackendWorker


REQUEST_STREAM = "worker:requests"
FAST_STREAM = "worker:fast"
SLOW_STREAM = "worker:slow"
ROUTER_GROUP = "worker-router"
FAST_GROUP = "worker-fast"
SLOW_GROUP = "worker-slow"
REPLY_PREFIX = "worker:reply:"
RESULT_STREAM = "applications:results"
REPLY_TTL_SECONDS = 60
FAST_WORKERS = 8
SLOW_WORKERS = 2
PENDING_IDLE_MS = 15 * 60 * 1000

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger(__name__)


class Worker:
    def __init__(self, client: redis.Redis[Any]) -> None:
        self.redis = client
        self.stop = threading.Event()
        self.consumer = f"{socket.gethostname()}-{os.getpid()}"

    def _create_groups(self) -> None:
        for stream, group in (
            (REQUEST_STREAM, ROUTER_GROUP),
            (FAST_STREAM, FAST_GROUP),
            (SLOW_STREAM, SLOW_GROUP),
        ):
            try:
                self.redis.xgroup_create(stream, group, id="0", mkstream=True)
            except redis.exceptions.ResponseError as exc:
                if not str(exc).startswith("BUSYGROUP"):
                    raise

    @staticmethod
    def _decode(fields: dict[str, str]) -> dict[str, Any]:
        task = json.loads(fields["task"])
        if not isinstance(task, dict) or not isinstance(task.get("id"), str) or not task["id"]:
            raise ValueError("task id is required")
        if not isinstance(task.get("kind"), str) or not task["kind"]:
            raise ValueError("task kind is required")
        if not isinstance(task.get("payload"), dict):
            raise ValueError("task payload must be an object")
        if "persist" in task and not isinstance(task["persist"], bool):
            raise ValueError("task persist must be a boolean")
        return task

    def _reply(self, stream: str, group: str, message_id: str, task_id: str | None,
               data: dict[str, Any] | None = None, error: str | None = None) -> None:
        pipe = self.redis.pipeline(transaction=True)
        if task_id:
            response = {"error": error} if error else {"data": data}
            key = REPLY_PREFIX + task_id
            pipe.lpush(key, json.dumps(response, ensure_ascii=False))
            pipe.expire(key, REPLY_TTL_SECONDS)
        pipe.xack(stream, group, message_id)
        pipe.execute()

    def _application_result(self, stream: str, group: str, message_id: str,
                            task: dict[str, Any], result: dict[str, Any] | None,
                            error: str | None) -> None:
        payload = task["payload"]
        event = {
            "event_id": task["id"], "job_id": task["id"],
            "application_id": payload["application_id"],
            "from": "SUBMITTED", "to": "DATA_CHECK",
            "kind": task["kind"], "result": result, "error": error,
            "comment": error or f"{task['kind']} completed",
        }
        if task["kind"] == "verify_application" and result:
            event["decision"] = {
                "debt_to_equity": result.get("debt_to_equity"),
                "risk_flags": result.get("risk_flags", []),
                "manual_review_required": result.get("manual_review_required", False),
            }
            if result.get("next_status") == "INFO_REQUESTED":
                event["info_request"] = {"missing_fields": result.get("missing_fields", [])}
        pipe = self.redis.pipeline(transaction=True)
        pipe.xadd(RESULT_STREAM, {"event": json.dumps(event, ensure_ascii=False)})
        pipe.xack(stream, group, message_id)
        pipe.execute()

    def _route_loop(self) -> None:
        while not self.stop.is_set():
            batches = self.redis.xreadgroup(
                ROUTER_GROUP, self.consumer + "-router", {REQUEST_STREAM: ">"}, count=10, block=1000
            )
            if not batches:
                _, pending, _ = self.redis.xautoclaim(
                    REQUEST_STREAM, ROUTER_GROUP, self.consumer + "-router",
                    min_idle_time=PENDING_IDLE_MS, start_id="0-0", count=10,
                )
                batches = [(REQUEST_STREAM, pending)]
            for _, messages in batches:
                for message_id, fields in messages:
                    try:
                        task = self._decode(fields)
                        lane = lane_for(task["kind"])
                    except (KeyError, TypeError, ValueError, json.JSONDecodeError) as exc:
                        task_id = None
                        try:
                            raw_task = json.loads(fields.get("task", "{}"))
                            if isinstance(raw_task, dict) and isinstance(raw_task.get("id"), str):
                                task_id = raw_task["id"]
                        except ValueError:
                            pass
                        self._reply(REQUEST_STREAM, ROUTER_GROUP, message_id, task_id, error=str(exc))
                        continue
                    destination = FAST_STREAM if lane == "fast" else SLOW_STREAM
                    pipe = self.redis.pipeline(transaction=True)
                    pipe.xadd(destination, fields)
                    pipe.xack(REQUEST_STREAM, ROUTER_GROUP, message_id)
                    pipe.execute()

    def _service_loop(self, stream: str, group: str, consumer: str) -> None:
        while not self.stop.is_set():
            batches = self.redis.xreadgroup(group, consumer, {stream: ">"}, count=1, block=1000)
            if not batches:
                _, pending, _ = self.redis.xautoclaim(
                    stream, group, consumer, min_idle_time=PENDING_IDLE_MS, start_id="0-0", count=1,
                )
                batches = [(stream, pending)]
            for _, messages in batches:
                for message_id, fields in messages:
                    task_id = None
                    task = None
                    try:
                        task = self._decode(fields)
                        task_id = task["id"]
                        expected = FAST_STREAM if lane_for(task["kind"]) == "fast" else SLOW_STREAM
                        if stream != expected:
                            raise ValueError("task was routed to the wrong lane")
                        payload = task["payload"]
                        result = dispatch(task["kind"], payload)
                        if task.get("persist"):
                            self._application_result(stream, group, message_id, task, result, None)
                        else:
                            self._reply(stream, group, message_id, task_id, data=result)
                    except (KeyError, TypeError, ValueError, json.JSONDecodeError) as exc:
                        log.warning("task %s failed validation: %s", message_id, exc)
                        if task_id and isinstance(task, dict) and task.get("persist") and isinstance(task.get("payload", {}).get("application_id"), str):
                            self._application_result(stream, group, message_id, task, None, str(exc))
                        else:
                            self._reply(stream, group, message_id, task_id, error=str(exc))

    def run(self) -> None:
        self.redis.ping()
        self._create_groups()
        log.info("Python worker ready: %d fast, %d slow services", FAST_WORKERS, SLOW_WORKERS)
        with ThreadPoolExecutor(max_workers=1 + FAST_WORKERS + SLOW_WORKERS) as pool:
            futures = [pool.submit(self._route_loop)]
            futures += [
                pool.submit(self._service_loop, FAST_STREAM, FAST_GROUP, f"{self.consumer}-fast-{i}")
                for i in range(FAST_WORKERS)
            ]
            futures += [
                pool.submit(self._service_loop, SLOW_STREAM, SLOW_GROUP, f"{self.consumer}-slow-{i}")
                for i in range(SLOW_WORKERS)
            ]
            while not self.stop.wait(0.2):
                for future in futures:
                    if future.done():
                        future.result()
                        raise RuntimeError("worker loop stopped unexpectedly")


def main() -> None:
    address = os.getenv("REDIS_ADDR", "localhost:6379")
    host, port = address.rsplit(":", 1)
    client: redis.Redis[Any] = redis.Redis(host=host, port=int(port), decode_responses=True)
    protocol = os.getenv("WORKER_PROTOCOL", "legacy")
    if protocol == "legacy":
        worker = Worker(client)
    elif protocol == "backend":
        worker = BackendWorker(client)
    else:
        raise ValueError("WORKER_PROTOCOL must be legacy or backend")
    signal.signal(signal.SIGTERM, lambda *_: worker.stop.set())
    signal.signal(signal.SIGINT, lambda *_: worker.stop.set())
    worker.run()


if __name__ == "__main__":
    main()
