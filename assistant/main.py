"""
FastAPI Microservice for the Credit Assistant Curator.
Provides REST API endpoints for chat, product recommendation, and catalog queries.
"""
from __future__ import annotations
from typing import List
from fastapi import FastAPI, APIRouter
from fastapi.middleware.cors import CORSMiddleware

from assistant.domain.models import (
    ProductSpec,
    ChatRequest,
    ChatResponse,
    RecommendRequest,
    RecommendResponse,
)
from assistant.domain.catalog import get_all_products, get_product_by_id
from assistant.engine.matcher import ProductMatcher
from assistant.engine.dialogue_manager import DialogueManager

app = FastAPI(
    title="Alfa Credit Product Assistant Curator",
    version="1.0.0",
    description="Intelligent AI assistant and curator for small & medium business loan products",
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

dialogue_manager = DialogueManager()

router = APIRouter()


@router.get("/healthz")
async def health_check():
    return {"status": "ok", "service": "assistant"}


@router.get("/products", response_model=List[ProductSpec])
async def list_products():
    return get_all_products()


@router.get("/products/{product_id}", response_model=ProductSpec)
async def get_product(product_id: str):
    p = get_product_by_id(product_id)
    if not p:
        from fastapi import HTTPException
        raise HTTPException(status_code=404, detail="Product not found")
    return p


@router.post("/chat", response_model=ChatResponse)
async def chat_endpoint(request: ChatRequest):
    return dialogue_manager.process(request)


@router.post("/recommend", response_model=RecommendResponse)
async def recommend_endpoint(request: RecommendRequest):
    offers = ProductMatcher.match(
        org=request.org,
        collateral=request.collateral,
        age_months=request.age_months,
        amount=request.amount,
        term_months=request.term_months,
        purpose=request.purpose,
        limit=3,
    )
    explanation = f"Найдено {len(offers)} подходящих кредитных продуктов."
    if offers:
        best = offers[0]
        explanation = f"Наиболее выгодный вариант — «{best.product.title}» со ставкой {best.product.rate:.1f}%."
    return RecommendResponse(offers=offers, explanation=explanation)


# Mount router both under /api/v1/assistant and at root for direct and proxied calls
app.include_router(router, prefix="/api/v1/assistant")
app.include_router(router)


@app.get("/healthz")
async def root_health_check():
    return {"status": "ok", "service": "assistant"}
