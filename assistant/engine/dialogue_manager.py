"""
Dialogue state manager (FSM) for the credit assistant curator.
Maintains multi-turn context, resolves slots, coordinates matching and NLG.
"""
from __future__ import annotations
from typing import Dict, Any, Optional
from assistant.domain.models import ChatRequest, ChatResponse, ClientContext, ProductOffer
from assistant.domain.catalog import get_all_products, get_product_by_id
from assistant.engine.slot_filler import SlotFiller, ExtractedSlots
from assistant.engine.matcher import ProductMatcher
from assistant.nlg import templates


class SessionState:
    def __init__(self, session_id: str):
        self.session_id = session_id
        self.slots = ExtractedSlots()
        self.dialogue_state = "IDLE"
        self.last_offers: list[ProductOffer] = []
        self.last_product_id: Optional[str] = None


class DialogueManager:
    """Manages active dialogue sessions and orchestrates NLU, matching, and NLG."""

    def __init__(self):
        self._sessions: Dict[str, SessionState] = {}

    def get_or_create_session(self, session_id: str) -> SessionState:
        if session_id not in self._sessions:
            self._sessions[session_id] = SessionState(session_id)
        return self._sessions[session_id]

    def process(self, req: ChatRequest) -> ChatResponse:
        session = self.get_or_create_session(req.session_id)
        text = (req.message or "").strip()
        ctx = req.context or ClientContext()

        # Update slots from incoming UI calculator context if present
        if ctx.calculator_state:
            cs = ctx.calculator_state
            if cs.amount and not session.slots.amount:
                session.slots.amount = cs.amount
            if cs.term and not session.slots.term_months:
                session.slots.term_months = cs.term
            if cs.org and not session.slots.org:
                session.slots.org = cs.org
            if cs.collateral and not session.slots.collateral:
                session.slots.collateral = cs.collateral
            if cs.age and not session.slots.age_category:
                session.slots.age_category = cs.age

        # Update business age from company context if present
        if ctx.business_age_months is not None:
            session.slots.age_months = ctx.business_age_months
            session.slots.age_category = "lt9" if ctx.business_age_months < 9 else "gte9"
        if ctx.org_type:
            session.slots.org = ctx.org_type.lower()

        # Extract slots from user text
        extracted = SlotFiller.extract(text)
        session.slots = session.slots.merge_with(extracted)

        # 1. Greeting
        if extracted.intent == "greeting" and not any([extracted.amount, extracted.term_months, extracted.purpose]):
            res = templates.render_greeting(ctx.page)
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 2. Status Help in Cabinet
        if extracted.intent == "status_inquiry" or (
            ctx.page == "cabinet" and ctx.application_status and any(w in text.lower() for w in ["статус", "заявк", "что с"])
        ):
            res = templates.render_status_help(
                status=ctx.application_status or "INFO_REQUESTED",
                missing_fields=ctx.missing_fields,
                missing_docs=ctx.missing_docs,
            )
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 3. Documents consultation
        if extracted.intent == "docs_consultation":
            target_product = None
            t_lower = text.lower()
            if "авто" in t_lower or "машин" in t_lower:
                target_product = get_product_by_id("auto")
            elif "оборудован" in t_lower or "станок" in t_lower:
                target_product = get_product_by_id("equipment")
            elif "маркетплейс" in t_lower or "wildberries" in t_lower or "ozon" in t_lower:
                target_product = get_product_by_id("seller")
            elif "ипотек" in t_lower:
                target_product = get_product_by_id("mortgage-ooo")
            elif "бизнес" in t_lower or "завод" in t_lower:
                target_product = get_product_by_id("ready-business")
            elif "овердрафт" in t_lower:
                target_product = get_product_by_id("overdraft")
            elif "вкл" in t_lower or "линия" in t_lower:
                target_product = get_product_by_id("credit-line")
            elif "оборот" in t_lower:
                target_product = get_product_by_id("working-capital")
            elif "экспресс" in t_lower:
                target_product = get_product_by_id("express")
            elif "до 1 млн" in t_lower or "до миллиона" in t_lower:
                target_product = get_product_by_id("small")

            # Fallback to session memory
            if not target_product and session.last_product_id:
                target_product = get_product_by_id(session.last_product_id)
            if not target_product and session.last_offers:
                target_product = session.last_offers[0].product
            if not target_product:
                target_product = get_product_by_id("working-capital")

            res = templates.render_docs(
                product_title=target_product.title,
                docs=target_product.required_docs,
            )
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 4. Product matching & recommendation
        is_young = (session.slots.age_months is not None and session.slots.age_months < 9) or (
            session.slots.age_category == "lt9"
        )

        offers = ProductMatcher.match(
            org=session.slots.org,
            collateral=session.slots.collateral,
            age_months=session.slots.age_months,
            age_category=session.slots.age_category,
            amount=session.slots.amount,
            term_months=session.slots.term_months,
            purpose=session.slots.purpose,
            limit=3,
        )

        session.last_offers = offers
        if offers:
            session.last_product_id = offers[0].product.id

        res = templates.render_recommendation(
            offers=offers,
            amount=session.slots.amount,
            term=session.slots.term_months,
            org=session.slots.org,
            age_category=session.slots.age_category,
            is_young=is_young,
        )

        session.dialogue_state = res["dialogue_state"]
        return ChatResponse(session_id=session.session_id, **res)
