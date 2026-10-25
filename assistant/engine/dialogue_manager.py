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
from assistant.domain.qa_knowledge_base import qa_engine
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

        # Handle empty message
        if not text:
            res = templates.render_greeting(ctx.page)
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # Extract slots from user text
        extracted = SlotFiller.extract(text)
        session.slots = session.slots.merge_with(extracted)

        # 0. Easter Egg Commands
        if extracted.intent == "easter_egg_welldone":
            res = templates.render_easter_egg("welldone")
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        if extracted.intent == "easter_egg_artem":
            res = templates.render_easter_egg("artem")
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        if extracted.intent == "easter_egg_ruslan":
            res = templates.render_easter_egg("ruslan")
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        if extracted.intent == "easter_egg_sanya":
            res = templates.render_easter_egg("sanya")
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        if extracted.intent == "easter_egg_max":
            res = templates.render_easter_egg("max")
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 0.1 Chit-Chat Commands
        if extracted.intent in (
            "chit_chat_how_are_you",
            "chit_chat_who_are_you",
            "chit_chat_joke",
            "chit_chat_advice",
            "chit_chat_farewell",
        ):
            res = templates.render_chit_chat(extracted.intent)
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 0.2 Bot Capabilities Guide
        if extracted.intent == "bot_capabilities":
            guide_item = qa_engine.get_by_id("assistant_capabilities_guide")
            if guide_item:
                res = templates.render_qa_answer(guide_item)
                session.dialogue_state = res["dialogue_state"]
                return ChatResponse(session_id=session.session_id, **res)

        # 1. Reset / Start Over
        if extracted.intent == "reset_flow":
            session.slots = ExtractedSlots()
            session.last_offers = []
            session.last_product_id = None
            res = templates.render_reset_flow()
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 2. Greeting
        if extracted.intent == "greeting" and not any([extracted.amount, extracted.term_months, extracted.purpose]):
            res = templates.render_greeting(ctx.page)
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 3. Gratitude / polite acknowledgment
        if extracted.intent == "gratitude":
            res = templates.render_gratitude()
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 4. Increase Amount Command
        if extracted.intent == "increase_amount":
            cur_p = get_product_by_id(session.last_product_id) if session.last_product_id else None
            is_y = (session.slots.age_months is not None and session.slots.age_months < 9) or (session.slots.age_category == "lt9")
            res = templates.render_increase_amount(product=cur_p, current_amount=session.slots.amount, is_young=is_y)
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 5. Change Term Command
        if extracted.intent == "change_term":
            cur_p = get_product_by_id(session.last_product_id) if session.last_product_id else None
            res = templates.render_change_term(product=cur_p, amount=session.slots.amount)
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 6. Comparison
        if extracted.intent == "comparison":
            p1, p2 = self._resolve_comparison_products(text, session)
            res = templates.render_comparison(p1, p2, session.last_offers)
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 7. FAQ Responses (standard shortcuts)
        if extracted.intent in ("faq_signing", "faq_timeline", "faq_manager"):
            res = templates.render_faq(extracted.intent)
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 8. All products catalog overview
        if extracted.intent == "all_products":
            res = templates.render_all_products()
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 9. Status Help in Cabinet
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

        # 10. Documents consultation
        if extracted.intent == "docs_consultation":
            target_product = self._resolve_target_product_for_docs(text, session)
            res = templates.render_docs(
                product_title=target_product.title,
                docs=target_product.required_docs,
            )
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 11. Domain QA Knowledge Base Search (Semantic Retrieval)
        # Trigger QA search if user asks an informative question or general banking topic,
        # but NOT when they provide concrete loan calculation parameters (amount/purpose).
        has_new_financial_slots = bool(extracted.amount or extracted.term_months or extracted.purpose)
        is_faq_query = (
            any(w in text.lower() for w in [
                "?", "можно", "почему", "какая", "какие", "штраф", "комисси", "досрочн",
                "оценк", "разниц", "отличи", "отказ", "неустойк", "каникул", "счет",
                "выписк", "убыток", "налог", "усн", "патент", "осно", "бенефициар",
                "рейтинг", "рефинансир", "лизинг", "пересчет"
            ])
            or not has_new_financial_slots
        )

        if is_faq_query and extracted.intent != "collect_slots" and not (extracted.amount and (extracted.purpose or session.slots.purpose)):
            qa_matches = qa_engine.search(text, top_k=1, threshold=1.2)
            if qa_matches:
                item, score = qa_matches[0]
                if score >= 1.5:
                    res = templates.render_qa_answer(item)
                    session.dialogue_state = res["dialogue_state"]
                    return ChatResponse(session_id=session.session_id, **res)

        # 12. General inquiry without slots -> Ask clarifying questions (COLLECTING_SLOTS)
        if extracted.intent == "collect_slots" and not any([session.slots.amount, session.slots.purpose]):
            res = templates.render_collect_slots()
            session.dialogue_state = res["dialogue_state"]
            return ChatResponse(session_id=session.session_id, **res)

        # 9. Product matching & recommendation
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
            target_product_id=session.slots.target_product_id,
            limit=3,
        )

        session.last_offers = offers
        if offers:
            session.last_product_id = offers[0].product.id

        is_auth = bool(ctx.is_authenticated) if ctx else False
        res = templates.render_recommendation(
            offers=offers,
            amount=session.slots.amount,
            term=session.slots.term_months,
            org=session.slots.org,
            age_category=session.slots.age_category,
            is_young=is_young,
            is_authenticated=is_auth,
        )

        session.dialogue_state = res["dialogue_state"]
        return ChatResponse(session_id=session.session_id, **res)

    def _resolve_comparison_products(self, text: str, session: SessionState):
        t = text.lower()
        candidates = []
        if "овердрафт" in t:
            p = get_product_by_id("overdraft")
            if p:
                candidates.append(p)
        if "вкл" in t or "лини" in t:
            p = get_product_by_id("credit-line")
            if p:
                candidates.append(p)
        if "оборот" in t:
            p = get_product_by_id("working-capital")
            if p:
                candidates.append(p)
        if "экспресс" in t:
            p = get_product_by_id("express")
            if p:
                candidates.append(p)
        if "до 1 млн" in t or "до миллиона" in t or "малы" in t:
            p = get_product_by_id("small")
            if p:
                candidates.append(p)
        if "маркетплейс" in t or "селлер" in t:
            p = get_product_by_id("seller")
            if p:
                candidates.append(p)
        if "авто" in t:
            p = get_product_by_id("auto")
            if p:
                candidates.append(p)
        if "ипотек" in t:
            p = get_product_by_id("mortgage-ooo")
            if p:
                candidates.append(p)
        if "залог" in t:
            p = get_product_by_id("secured-property")
            if p:
                candidates.append(p)

        if len(candidates) >= 2:
            return candidates[0], candidates[1]

        # Use last offers if available
        if session.last_offers and len(session.last_offers) >= 2:
            return session.last_offers[0].product, session.last_offers[1].product

        if candidates and session.last_offers:
            first = candidates[0]
            second = next((o.product for o in session.last_offers if o.product.id != first.id), None)
            if second:
                return first, second

        # Standard informative fallback comparison: Overdraft vs Credit Line
        p1 = get_product_by_id("overdraft") or get_all_products()[6]
        p2 = get_product_by_id("credit-line") or get_all_products()[7]
        return p1, p2

    def _resolve_target_product_for_docs(self, text: str, session: SessionState):
        t_lower = text.lower()
        if "авто" in t_lower or "машин" in t_lower:
            return get_product_by_id("auto")
        if "оборудован" in t_lower or "станок" in t_lower:
            return get_product_by_id("equipment")
        if "маркетплейс" in t_lower or "wildberries" in t_lower or "ozon" in t_lower or "селлер" in t_lower:
            return get_product_by_id("seller")
        if "ипотек" in t_lower:
            if "ип" in t_lower and "ооо" not in t_lower:
                return get_product_by_id("mortgage-ip")
            return get_product_by_id("mortgage-ooo")
        if "бизнес" in t_lower or "завод" in t_lower:
            return get_product_by_id("ready-business")
        if "овердрафт" in t_lower:
            return get_product_by_id("overdraft")
        if "вкл" in t_lower or "линия" in t_lower:
            return get_product_by_id("credit-line")
        if "экспресс" in t_lower:
            return get_product_by_id("express")
        if "до 1 млн" in t_lower or "до миллиона" in t_lower:
            return get_product_by_id("small")
        if "для ооо без залога" in t_lower:
            return get_product_by_id("ooo-unsecured")
        if "для ип без залога" in t_lower:
            return get_product_by_id("ip-unsecured")
        if "оборот" in t_lower:
            return get_product_by_id("working-capital")
        if "залог" in t_lower:
            return get_product_by_id("secured-property")

        # Fallback to session memory
        if session.last_product_id:
            p = get_product_by_id(session.last_product_id)
            if p:
                return p
        if session.last_offers:
            return session.last_offers[0].product
        return get_product_by_id("working-capital") or get_all_products()[0]
