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


REQUEST_STREAM = "worker:requests"
FAST_STREAM = "worker:fast"
SLOW_STREAM = "worker:slow"
ROUTER_GROUP = "worker-router"
FAST_GROUP = "worker-fast"
SLOW_GROUP = "worker-slow"
REPLY_PREFIX = "worker:reply:"
REPLY_TTL_SECONDS = 60
FAST_WORKERS = 8
SLOW_WORKERS = 2

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

    def _route_loop(self) -> None:
        while not self.stop.is_set():
            batches = self.redis.xreadgroup(
                ROUTER_GROUP, self.consumer + "-router", {REQUEST_STREAM: ">"}, count=10, block=1000
            )
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
            for _, messages in batches:
                for message_id, fields in messages:
                    task_id = None
                    try:
                        task = self._decode(fields)
                        task_id = task["id"]
                        expected = FAST_STREAM if lane_for(task["kind"]) == "fast" else SLOW_STREAM
                        if stream != expected:
                            raise ValueError("task was routed to the wrong lane")
                        result = dispatch(task["kind"], task["payload"])
                        self._reply(stream, group, message_id, task_id, data=result)
                    except (KeyError, TypeError, ValueError, json.JSONDecodeError) as exc:
                        log.warning("task %s failed validation: %s", message_id, exc)
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
    worker = Worker(client)
    signal.signal(signal.SIGTERM, lambda *_: worker.stop.set())
    signal.signal(signal.SIGINT, lambda *_: worker.stop.set())
    worker.run()


if __name__ == "__main__":
    main()
