// components/baseui/composite.js
// Port of @base-ui/react internals/composite (1.6.0): useCompositeRoot.ts,
// useCompositeItem.ts and composite.ts, with floating-ui-react's
// utils/composite.ts list helpers, which use_list_navigation.js shares. One tab stop for a group of items,
// the arrow keys move it by orientation, optionally Home and End, with loop.
//
//   const composite = window.templ.composite.useCompositeRoot(root, options)
//   composite.highlight(index)   onHighlightedIndexChange from the component
//   composite.index()            the highlighted index
//   composite.cleanup()
//
//   items()                the items in order (elementsRef)
//   loopFocus              default true
//   orientation            "horizontal", "vertical" or "both" (default)
//   rtl()                  the direction
//   enableHomeAndEndKeys   default false
//   stopEventPropagation   default true, CompositeRoot's
//   disabledIndices        an array, a function, or undefined for the DOM
//   modifierKeys           modifiers that do not cancel the navigation
//   highlightItemOnHover   default false
//   onHighlightedIndexChange(index)   after the highlight moved
//
// The root sets the default tab stop once on creation (onMapChange): the item
// with data-composite-item-active, or the first enabled one when the first
// is disabled. The items' tabIndex follows the highlight.
(function () {
  "use strict";

  const ACTIVE_COMPOSITE_ITEM = "data-composite-item-active";
  const ARROW_UP = "ArrowUp";
  const ARROW_DOWN = "ArrowDown";
  const ARROW_LEFT = "ArrowLeft";
  const ARROW_RIGHT = "ArrowRight";
  const HOME = "Home";
  const END = "End";
  const HORIZONTAL_KEYS = new Set([ARROW_LEFT, ARROW_RIGHT]);
  const HORIZONTAL_KEYS_WITH_EXTRA_KEYS = new Set([ARROW_LEFT, ARROW_RIGHT, HOME, END]);
  const VERTICAL_KEYS = new Set([ARROW_UP, ARROW_DOWN]);
  const VERTICAL_KEYS_WITH_EXTRA_KEYS = new Set([ARROW_UP, ARROW_DOWN, HOME, END]);
  const ARROW_KEYS = new Set([...HORIZONTAL_KEYS, ...VERTICAL_KEYS]);
  const COMPOSITE_KEYS = new Set([...ARROW_KEYS, HOME, END]);
  const MODIFIER_KEYS = new Set(["Shift", "Control", "Alt", "Meta"]);

  const t = () => window.templ.tabbable;

  // ----- utils/composite.ts: the list helpers, shared with use_list_navigation.js

  function isIndexOutOfListBounds(list, index) {
    return index < 0 || index >= list.length;
  }

  function isListIndexDisabled(list, index, disabledIndices) {
    const isExplicitlyDisabled = typeof disabledIndices === "function"
      ? disabledIndices(index)
      : disabledIndices?.includes(index) ?? false;
    if (isExplicitlyDisabled) return true;
    const element = list[index];
    if (!element) return false;
    if (!t().isElementVisible(element)) return true;
    return !disabledIndices && (element.hasAttribute("disabled") || element.getAttribute("aria-disabled") === "true");
  }

  function findNonDisabledListIndex(list, { startingIndex = -1, decrement = false, disabledIndices, amount = 1 } = {}) {
    let index = startingIndex;
    do {
      index += decrement ? -amount : amount;
    } while (index >= 0 && index <= list.length - 1 && isListIndexDisabled(list, index, disabledIndices));
    return index;
  }

  function getMinListIndex(list, disabledIndices) {
    return findNonDisabledListIndex(list, { disabledIndices });
  }

  function getMaxListIndex(list, disabledIndices) {
    return findNonDisabledListIndex(list, { decrement: true, startingIndex: list.length, disabledIndices });
  }

  const list = () => ({ isIndexOutOfListBounds, isListIndexDisabled, findNonDisabledListIndex, getMinListIndex, getMaxListIndex });

  function isNativeInput(element) {
    if (element instanceof HTMLElement && element.tagName === "INPUT" && element.selectionStart != null) return true;
    return element instanceof HTMLElement && element.tagName === "TEXTAREA";
  }

  function isElementDisabled(element) {
    return element == null || element.hasAttribute("disabled") || element.getAttribute("aria-disabled") === "true";
  }

  function getOffset(ancestor, element, side) {
    const propName = side === "left" ? "offsetLeft" : "offsetTop";
    let result = 0;
    while (element.offsetParent) {
      result += element[propName];
      if (element.offsetParent === ancestor) break;
      element = element.offsetParent;
    }
    return result;
  }

  function getStyles(element) {
    const styles = getComputedStyle(element);
    return {
      scrollMarginTop: parseFloat(styles.scrollMarginTop) || 0,
      scrollMarginRight: parseFloat(styles.scrollMarginRight) || 0,
      scrollMarginBottom: parseFloat(styles.scrollMarginBottom) || 0,
      scrollMarginLeft: parseFloat(styles.scrollMarginLeft) || 0,
      scrollPaddingTop: parseFloat(styles.scrollPaddingTop) || 0,
      scrollPaddingRight: parseFloat(styles.scrollPaddingRight) || 0,
      scrollPaddingBottom: parseFloat(styles.scrollPaddingBottom) || 0,
      scrollPaddingLeft: parseFloat(styles.scrollPaddingLeft) || 0,
    };
  }

  function scrollIntoViewIfNeeded(scrollContainer, element, direction, orientation) {
    if (!scrollContainer || !element || !element.scrollTo) return;
    let targetX = scrollContainer.scrollLeft;
    let targetY = scrollContainer.scrollTop;
    const isOverflowingX = scrollContainer.clientWidth < scrollContainer.scrollWidth;
    const isOverflowingY = scrollContainer.clientHeight < scrollContainer.scrollHeight;
    if (isOverflowingX && orientation !== "vertical") {
      const elementOffsetLeft = getOffset(scrollContainer, element, "left");
      const containerStyles = getStyles(scrollContainer);
      const elementStyles = getStyles(element);
      const overflowsRight = elementOffsetLeft + element.offsetWidth + elementStyles.scrollMarginRight >
        scrollContainer.scrollLeft + scrollContainer.clientWidth - containerStyles.scrollPaddingRight;
      const alignRight = elementOffsetLeft + element.offsetWidth + elementStyles.scrollMarginRight -
        scrollContainer.clientWidth + containerStyles.scrollPaddingRight;
      const alignLeft = elementOffsetLeft - elementStyles.scrollMarginLeft - containerStyles.scrollPaddingLeft;
      if (direction === "ltr") {
        if (overflowsRight) targetX = alignRight;
        else if (elementOffsetLeft - elementStyles.scrollMarginLeft < scrollContainer.scrollLeft + containerStyles.scrollPaddingLeft) targetX = alignLeft;
      }
      if (direction === "rtl") {
        if (elementOffsetLeft - elementStyles.scrollMarginRight < scrollContainer.scrollLeft + containerStyles.scrollPaddingLeft) targetX = alignLeft;
        else if (overflowsRight) targetX = alignRight;
      }
    }
    if (isOverflowingY && orientation !== "horizontal") {
      const elementOffsetTop = getOffset(scrollContainer, element, "top");
      const containerStyles = getStyles(scrollContainer);
      const elementStyles = getStyles(element);
      if (elementOffsetTop - elementStyles.scrollMarginTop < scrollContainer.scrollTop + containerStyles.scrollPaddingTop) {
        targetY = elementOffsetTop - elementStyles.scrollMarginTop - containerStyles.scrollPaddingTop;
      } else if (elementOffsetTop + element.offsetHeight + elementStyles.scrollMarginBottom >
        scrollContainer.scrollTop + scrollContainer.clientHeight - containerStyles.scrollPaddingBottom) {
        targetY = elementOffsetTop + element.offsetHeight + elementStyles.scrollMarginBottom -
          scrollContainer.clientHeight + containerStyles.scrollPaddingBottom;
      }
    }
    scrollContainer.scrollTo({ left: targetX, top: targetY, behavior: "auto" });
  }

  function isModifierKeySet(event, ignoredModifierKeys) {
    for (const key of MODIFIER_KEYS.values()) {
      if (ignoredModifierKeys.includes(key)) continue;
      if (event.getModifierState(key)) return true;
    }
    return false;
  }

  function useCompositeRoot(root, options) {
    const {
      items,
      loopFocus = true,
      orientation = "both",
      rtl = () => false,
      enableHomeAndEndKeys = false,
      stopEventPropagation = true,
      disabledIndices,
      modifierKeys = [],
      highlightItemOnHover = false,
      onHighlightedIndexChange: onChange,
    } = options;
    let highlightedIndex = 0;
    const direction = () => (rtl() ? "rtl" : "ltr");

    // useCompositeItem's tabIndex on every item.
    function applyTabIndex() {
      items().forEach((item, index) => {
        item.tabIndex = index === highlightedIndex ? 0 : -1;
      });
    }

    function onHighlightedIndexChange(index, shouldScrollIntoView = false) {
      highlightedIndex = index;
      applyTabIndex();
      if (shouldScrollIntoView) scrollIntoViewIfNeeded(root, items()[index], direction(), orientation);
      onChange?.(index);
    }

    // onMapChange: the first population sets the default tab stop.
    const elements = items();
    const activeItem = elements.find((element) => element.hasAttribute(ACTIVE_COMPOSITE_ITEM)) ?? null;
    const activeIndex = activeItem ? elements.indexOf(activeItem) : -1;
    if (activeIndex !== -1) {
      highlightedIndex = activeIndex;
    } else if (list().isListIndexDisabled(elements, highlightedIndex, disabledIndices)) {
      // A disabled item does not hold the single tab stop: move it to the
      // first enabled one, or keep it when every item is disabled.
      const firstEnabledIndex = list().findNonDisabledListIndex(elements, { disabledIndices });
      if (!list().isIndexOutOfListBounds(elements, firstEnabledIndex)) highlightedIndex = firstEnabledIndex;
    }
    applyTabIndex();
    scrollIntoViewIfNeeded(root, activeItem, direction(), orientation);

    function onKeyDown(event) {
      const RELEVANT_KEYS = enableHomeAndEndKeys ? COMPOSITE_KEYS : ARROW_KEYS;
      if (!RELEVANT_KEYS.has(event.key)) return;
      if (isModifierKeySet(event, modifierKeys)) return;
      const elementsList = items();
      const isRtl = rtl();
      const horizontalForwardKey = isRtl ? ARROW_LEFT : ARROW_RIGHT;
      const horizontalBackwardKey = isRtl ? ARROW_RIGHT : ARROW_LEFT;
      const forwardKey = { horizontal: horizontalForwardKey, vertical: ARROW_DOWN, both: horizontalForwardKey }[orientation];
      const backwardKey = { horizontal: horizontalBackwardKey, vertical: ARROW_UP, both: horizontalBackwardKey }[orientation];
      const target = event.target;
      if (target != null && isNativeInput(target) && !isElementDisabled(target)) {
        const selectionStart = target.selectionStart;
        const selectionEnd = target.selectionEnd;
        const textContent = target.value ?? "";
        // Native textbox behavior: a text selection, arrowing forward before
        // the end, or backward after the start.
        if (selectionStart == null || event.shiftKey || selectionStart !== selectionEnd) return;
        if (event.key !== backwardKey && selectionStart < textContent.length) return;
        if (event.key !== forwardKey && selectionStart > 0) return;
      }
      let nextIndex = highlightedIndex;
      const minIndex = list().getMinListIndex(elementsList, disabledIndices);
      const maxIndex = list().getMaxListIndex(elementsList, disabledIndices);
      const forwardKeys = { horizontal: [horizontalForwardKey], vertical: [ARROW_DOWN], both: [horizontalForwardKey, ARROW_DOWN] }[orientation];
      const backwardKeys = { horizontal: [horizontalBackwardKey], vertical: [ARROW_UP], both: [horizontalBackwardKey, ARROW_UP] }[orientation];
      const preventedKeys = {
        horizontal: enableHomeAndEndKeys ? HORIZONTAL_KEYS_WITH_EXTRA_KEYS : HORIZONTAL_KEYS,
        vertical: enableHomeAndEndKeys ? VERTICAL_KEYS_WITH_EXTRA_KEYS : VERTICAL_KEYS,
        both: RELEVANT_KEYS,
      }[orientation];
      if (enableHomeAndEndKeys) {
        if (event.key === HOME) nextIndex = minIndex;
        else if (event.key === END) nextIndex = maxIndex;
      }
      if (nextIndex === highlightedIndex && (forwardKeys.includes(event.key) || backwardKeys.includes(event.key))) {
        if (loopFocus && nextIndex === maxIndex && forwardKeys.includes(event.key)) {
          nextIndex = minIndex;
        } else if (loopFocus && nextIndex === minIndex && backwardKeys.includes(event.key)) {
          nextIndex = maxIndex;
        } else {
          nextIndex = list().findNonDisabledListIndex(elementsList, {
            startingIndex: nextIndex,
            decrement: backwardKeys.includes(event.key),
            disabledIndices,
          });
        }
      }
      if (nextIndex !== highlightedIndex && !list().isIndexOutOfListBounds(elementsList, nextIndex)) {
        if (stopEventPropagation) event.stopPropagation();
        if (preventedKeys.has(event.key)) event.preventDefault();
        onHighlightedIndexChange(nextIndex, true);
        // After a focus manager's return focus.
        queueMicrotask(() => items()[nextIndex]?.focus());
      }
    }

    // The root's onFocus: a native input selects its text.
    function onFocus(event) {
      if (isNativeInput(event.target)) event.target.setSelectionRange(0, event.target.value.length ?? 0);
    }

    // useCompositeItem's onFocus and onMouseMove, delegated.
    function itemOf(target) {
      return items().find((item) => item === target || item.contains(target)) ?? null;
    }

    function onItemFocus(event) {
      const item = itemOf(event.target);
      if (item) onHighlightedIndexChange(items().indexOf(item));
    }

    function onItemMouseMove(event) {
      if (!highlightItemOnHover) return;
      const item = itemOf(event.target);
      if (!item) return;
      const disabled = item.hasAttribute("disabled") || item.getAttribute("aria-disabled") === "true";
      if (items().indexOf(item) !== highlightedIndex && !disabled) item.focus();
    }

    root.addEventListener("keydown", onKeyDown);
    root.addEventListener("focusin", onFocus);
    root.addEventListener("focusin", onItemFocus);
    root.addEventListener("mousemove", onItemMouseMove);

    return {
      highlight: (index) => onHighlightedIndexChange(index),
      index: () => highlightedIndex,
      cleanup() {
        root.removeEventListener("keydown", onKeyDown);
        root.removeEventListener("focusin", onFocus);
        root.removeEventListener("focusin", onItemFocus);
        root.removeEventListener("mousemove", onItemMouseMove);
      },
    };
  }

  window.templ = window.templ || {};
  window.templ.composite = {
    useCompositeRoot,
    ACTIVE_COMPOSITE_ITEM,
    isIndexOutOfListBounds,
    isListIndexDisabled,
    findNonDisabledListIndex,
    getMinListIndex,
    getMaxListIndex,
  };
})();

// components/baseui/floating_focus_manager.js
// Port of @base-ui/react floating-ui-react/components/FloatingFocusManager.tsx
// (1.6.0), with utils/FocusGuard.tsx and floating-ui-react/utils/enqueueFocus.ts.
//
// The component's effects become one handle that follows the popup:
//
//   const focus = window.templ.focusManager.useFloatingFocusManager(options)  on mount
//   focus.close(details)   when it closes, details { reason, event }
//   focus.unmount()        once it unmounted after its exit animation
//
//   floating                  the floating element (positioner, or the popup itself)
//   reference                 the trigger, the source's domReference
//   triggers                  every trigger of the popup (a list), default [reference]
//   modal                     traps focus with guards and hides the outside (default true)
//   initialFocus              true, false, an element, or fn(interactionType)
//   returnFocus               true, false, an element, or fn(closeType)
//   restoreFocus              false, true, or "popup"
//   closeOnFocusOut           default true
//   openInteractionType       "mouse", "touch", "pen", "keyboard", "", or null (programmatic)
//   previousFocusableElement, nextFocusableElement
//   getInsideElements         fn returning elements that count as inside
//   onOpenChange(open, reason, event)   requests the close
//
// The floating tree is the portal owner chain, like in use_dismiss.js: a
// child is an open focus manager whose floating element lies inside this one.
// The portal context is the [data-base-ui-portal] node around the floating
// element, see portal.js.
(function () {
  "use strict";

  const t = () => window.templ.tabbable;
  const webkit = typeof CSS !== "undefined" && !!CSS.supports?.("-webkit-backdrop-filter:none");
  const FOCUSABLE_ATTRIBUTE = "data-base-ui-focusable";
  const CLICK_TRIGGER_IDENTIFIER = "data-base-ui-click-trigger";
  const TYPEABLE_SELECTOR = "input:not([type='hidden']):not([disabled]),[contenteditable]:not([contenteditable='false']),textarea:not([disabled])";

  const instances = new Set();

  // ----- enqueueFocus -------------------------------------------------------

  // Returns a cancel for the queued focus.
  let rafId = 0;
  function enqueueFocus(el, options = {}) {
    const { preventScroll = false, sync = false, shouldFocus } = options;
    cancelAnimationFrame(rafId);
    const exec = () => {
      if (shouldFocus && !shouldFocus()) return;
      el?.focus({ preventScroll });
    };
    if (sync) {
      exec();
      return () => {};
    }
    const currentRafId = requestAnimationFrame(exec);
    rafId = currentRafId;
    return () => {
      if (rafId === currentRafId) {
        cancelAnimationFrame(currentRafId);
        rafId = 0;
      }
    };
  }

  // ----- FocusGuard -----------------------------------------------------------

  // A visually hidden tabbable span that hands focus on when it receives it.
  function createFocusGuard(type, onFocus) {
    const guard = document.createElement("span");
    if (type) guard.setAttribute("data-type", type);
    guard.setAttribute("aria-hidden", "true");
    guard.setAttribute("tabindex", "0");
    guard.setAttribute("data-base-ui-focus-guard", "");
    guard.style.cssText = "clip-path:inset(50%);overflow:hidden;white-space:nowrap;border:0;padding:0;width:1px;height:1px;margin:-1px;position:fixed;top:0;left:0";
    if (onFocus) guard.addEventListener("focus", onFocus);
    return guard;
  }

  // ----- helpers from the source's utils --------------------------------------

  const getTarget = (event) => event.composedPath?.()[0] || event.target;
  const isHTMLElement = (node) => node instanceof HTMLElement;
  const isTypeableElement = (element) => isHTMLElement(element) && element.matches(TYPEABLE_SELECTOR);
  const isTypeableCombobox = (element) => !!element && element.getAttribute("role") === "combobox" && isTypeableElement(element);
  const stopEvent = (event) => {
    event.preventDefault();
    event.stopPropagation();
  };

  // The element that takes focus: the one marked focusable inside a
  // positioning wrapper, or the floating element itself.
  function getFloatingFocusElement(floatingElement) {
    if (!floatingElement) return null;
    return floatingElement.hasAttribute(FOCUSABLE_ATTRIBUTE)
      ? floatingElement
      : floatingElement.querySelector(`[${FOCUSABLE_ATTRIBUTE}]`) || floatingElement;
  }

  function getFirstTabbableElement(container) {
    if (!container) return null;
    if (t().isTabbable(container)) return container;
    return t().tabbable(container)[0] || container;
  }

  // The React tree pendant, see use_dismiss.js.
  function withinTree(root, target) {
    for (let node = target; node; node = window.templ.portal.treeParent(node)) {
      if (node === root) return true;
    }
    return false;
  }

  function isVirtualClick(event) {
    if (event.pointerType === "" && event.isTrusted) return true;
    return event.detail === 0 && !event.pointerType;
  }

  function isVirtualPointerEvent(event) {
    return (event.width === 0 && event.height === 0) ||
      (event.width < 1 && event.height < 1 && event.pressure === 0 && event.detail === 0 && event.pointerType === "touch");
  }

  function getEventType(event, lastInteractionType) {
    if (!event) return lastInteractionType || "";
    if (event instanceof KeyboardEvent) return "keyboard";
    if (event instanceof FocusEvent) return lastInteractionType || "keyboard";
    if ("pointerType" in event) return event.pointerType || "keyboard";
    if ("touches" in event) return "touch";
    if (event instanceof MouseEvent) return lastInteractionType || (event.detail === 0 ? "keyboard" : "mouse");
    return "";
  }

  // The elements focused before a modal popup took focus, most recent last.
  const LIST_LIMIT = 20;
  let previouslyFocusedElements = [];
  function clearDisconnectedPreviouslyFocusedElements() {
    previouslyFocusedElements = previouslyFocusedElements.filter((entry) => entry.deref()?.isConnected);
  }
  function addPreviouslyFocusedElement(element) {
    clearDisconnectedPreviouslyFocusedElements();
    if (element && element.nodeName.toLowerCase() !== "body") {
      previouslyFocusedElements.push(new WeakRef(element));
      if (previouslyFocusedElements.length > LIST_LIMIT) previouslyFocusedElements = previouslyFocusedElements.slice(-LIST_LIMIT);
    }
  }
  function getPreviouslyFocusedElement() {
    clearDisconnectedPreviouslyFocusedElements();
    return previouslyFocusedElements[previouslyFocusedElements.length - 1]?.deref();
  }

  // A dialog without tabbable content takes the Tab stop itself. data-tabindex
  // marks the value Base UI wrote, so a user authored tabindex is left alone.
  function handleTabIndex(floatingFocusElement) {
    if (floatingFocusElement.hasAttribute("tabindex") && !floatingFocusElement.hasAttribute("data-tabindex")) return;
    if (!floatingFocusElement.getAttribute("role")?.includes("dialog")) return;
    const tabbableContent = t().focusable(floatingFocusElement).filter((element) => {
      const dataTabIndex = element.getAttribute("data-tabindex") || "";
      return t().isTabbable(element) || (element.hasAttribute("data-tabindex") && !dataTabIndex.startsWith("-"));
    });
    const tabIndex = floatingFocusElement.getAttribute("tabindex");
    if (tabbableContent.length === 0) {
      if (tabIndex !== "0") {
        floatingFocusElement.setAttribute("tabindex", "0");
        floatingFocusElement.setAttribute("data-tabindex", "0");
      }
    } else if (tabIndex !== "-1" || (floatingFocusElement.hasAttribute("data-tabindex") && floatingFocusElement.getAttribute("data-tabindex") !== "-1")) {
      floatingFocusElement.setAttribute("tabindex", "-1");
      floatingFocusElement.setAttribute("data-tabindex", "-1");
    }
  }

  // ----- the focus manager ------------------------------------------------------

  function useFloatingFocusManager(options) {
    const {
      floating,
      reference: domReference = null,
      modal = true,
      initialFocus = true,
      returnFocus = true,
      restoreFocus = false,
      closeOnFocusOut = true,
      openInteractionType = "",
      previousFocusableElement = null,
      nextFocusableElement = null,
      getInsideElements,
      onOpenChange,
    } = options;
    const triggers = [...(options.triggers || [domReference])].filter(Boolean);
    const doc = floating.ownerDocument;
    const floatingFocusElement = getFloatingFocusElement(floating);
    const portalNode = floating.closest("[data-base-ui-portal]");
    const ignoreInitialFocus = initialFocus === false;
    // A typeable combobox reference keeps focus in its input: no guards,
    // but the outside is still hidden.
    const isUntrappedTypeableCombobox = isTypeableCombobox(domReference) && ignoreInitialFocus;
    const self = { floating, reference: domReference, open: false };
    const cleanups = [];
    const openCleanups = [];

    let preventReturnFocus = false;
    let isPointerDown = false;
    let pointerDownOutside = false;
    let lastFocusedTabbable = null;
    let closeType = "";
    let lastInteractionType = "";
    let insideReactTree = false;
    let blurTimer = 0;
    let pointerDownTimer = 0;

    const getTabbableContent = (container = floatingFocusElement) => (container ? t().tabbable(container) : []);
    const getResolvedInsideElements = () => getInsideElements?.().filter((element) => element != null) ?? [];
    const on = (list, target, type, listener, capture) => {
      target.addEventListener(type, listener, !!capture);
      list.push(() => target.removeEventListener(type, listener, !!capture));
    };
    const isChild = (other) => other !== self && withinTree(floating, other.floating);
    const isAncestor = (other) => other !== self && withinTree(other.floating, floating);

    // Guards inside the floating tree: a modal trap, or for a non modal popup
    // in a portal the way back out through the portal's outside guards.
    const shouldRenderGuards = (modal ? !isUntrappedTypeableCombobox : true) && (portalNode != null || modal);
    let beforeGuard = null;
    let afterGuard = null;
    if (shouldRenderGuards) {
      beforeGuard = createFocusGuard("inside", (event) => {
        if (modal) {
          const els = getTabbableContent();
          enqueueFocus(els[els.length - 1]);
        } else if (portalNode) {
          preventReturnFocus = false;
          if (t().isOutsideEvent(event, portalNode)) {
            t().getNextTabbable(domReference)?.focus();
          } else {
            (previousFocusableElement || portalNode._templPortalGuards?.beforeOutside)?.focus();
          }
        }
      });
      afterGuard = createFocusGuard("inside", (event) => {
        if (modal) {
          enqueueFocus(getTabbableContent()[0]);
        } else if (portalNode) {
          if (closeOnFocusOut) preventReturnFocus = true;
          if (t().isOutsideEvent(event, portalNode)) {
            t().getPreviousTabbable(domReference)?.focus();
          } else {
            (nextFocusableElement || portalNode._templPortalGuards?.afterOutside)?.focus();
          }
        }
      });
      floatingFocusElement.before(beforeGuard);
      floatingFocusElement.after(afterGuard);
    }

    // Prevent Tab from escaping the modal when there is nothing tabbable.
    if (modal) {
      on(cleanups, doc, "keydown", (event) => {
        if (event.key === "Tab" && t().contains(floatingFocusElement, t().activeElement(doc)) &&
          getTabbableContent().length === 0 && !isUntrappedTypeableCombobox) {
          stopEvent(event);
        }
      });
    }

    // Close on focus out, and restore focus inside the floating tree.
    if (closeOnFocusOut) {
      const handlePointerDown = () => {
        isPointerDown = true;
        clearTimeout(pointerDownTimer);
        pointerDownTimer = setTimeout(() => {
          isPointerDown = false;
        }, 0);
      };
      const handleFocusIn = (event) => {
        const target = getTarget(event);
        if (t().isTabbable(target)) lastFocusedTabbable = target;
      };
      const handleFocusOutside = (event) => {
        const relatedTarget = event.relatedTarget;
        const currentTarget = event.currentTarget;
        const target = getTarget(event);
        // Focus lost to the body (a backdrop press): remember what had it, so a
        // confirmation dialog opened then can return focus there.
        if (modal && relatedTarget == null && target != null && t().contains(floating, target)) {
          addPreviouslyFocusedElement(target);
        }
        queueMicrotask(() => {
          const insideElements = getResolvedInsideElements();
          const portalGuards = portalNode?._templPortalGuards;
          const isRelatedFocusGuard = relatedTarget?.hasAttribute?.("data-base-ui-focus-guard") &&
            [beforeGuard, afterGuard, portalGuards?.beforeOutside, portalGuards?.afterOutside, previousFocusableElement, nextFocusableElement].includes(relatedTarget);
          const insideChild = [...instances].some((other) => isChild(other) && t().contains(other.floating, relatedTarget));
          // An ancestor's popup or its reference, not the ancestor's items.
          const toAncestor = [...instances].some((other) => isAncestor(other) &&
            ([other.floating, getFloatingFocusElement(other.floating)].includes(relatedTarget) || other.reference === relatedTarget));
          const movedToUnrelatedNode = !(
            t().contains(domReference, relatedTarget) ||
            withinTree(floating, relatedTarget) ||
            t().contains(relatedTarget, floating) ||
            t().contains(portalNode, relatedTarget) ||
            insideElements.some((element) => element === relatedTarget || t().contains(element, relatedTarget)) ||
            (relatedTarget != null && triggers.includes(relatedTarget)) ||
            triggers.some((trigger) => t().contains(trigger, relatedTarget)) ||
            isRelatedFocusGuard ||
            insideChild ||
            toAncestor
          );
          if (currentTarget === domReference && floatingFocusElement) handleTabIndex(floatingFocusElement);

          // Focus lost outside the floating tree when its element went away.
          if (restoreFocus && currentTarget !== domReference && !t().isElementVisible(target) && t().activeElement(doc) === doc.body) {
            if (isHTMLElement(floatingFocusElement)) {
              floatingFocusElement.focus();
              if (restoreFocus === "popup") {
                requestAnimationFrame(() => floatingFocusElement.focus());
                return;
              }
            }
            const tabbableContent = getTabbableContent();
            const prevTabbable = lastFocusedTabbable;
            const nodeToFocus = (prevTabbable && tabbableContent.includes(prevTabbable) ? prevTabbable : null) ||
              tabbableContent[tabbableContent.length - 1] || floatingFocusElement;
            if (isHTMLElement(nodeToFocus)) nodeToFocus.focus();
          }

          if (insideReactTree) {
            insideReactTree = false;
            return;
          }

          // Focus moved out of the floating tree with no portal guard to handle it.
          if ((isUntrappedTypeableCombobox ? true : !modal) && relatedTarget && movedToUnrelatedNode && !isPointerDown &&
            (isUntrappedTypeableCombobox || relatedTarget !== getPreviouslyFocusedElement())) {
            preventReturnFocus = true;
            onOpenChange?.(false, "focus-out", event);
          }
        });
      };
      // Focus leaving through a portaled child still counts as inside.
      const markInsideReactTree = () => {
        if (pointerDownOutside) return;
        insideReactTree = true;
        clearTimeout(blurTimer);
        blurTimer = setTimeout(() => {
          insideReactTree = false;
        }, 0);
      };
      if (isHTMLElement(domReference)) {
        on(cleanups, domReference, "focusout", handleFocusOutside);
        on(cleanups, domReference, "pointerdown", handlePointerDown);
      }
      on(cleanups, floating, "focusin", handleFocusIn);
      on(cleanups, floating, "focusout", handleFocusOutside);
      if (portalNode) on(cleanups, floating, "focusout", markInsideReactTree, true);
    }

    // The effects that run while open. A popup that opens again during its
    // exit animation runs them again on the same manager.
    function open() {
      self.open = true;
      // The portal's outside guards render first, they count as inside below.
      window.templ.portal.setFocusManagerState(portalNode, { ...portalNode?._templFocusState, open: true });
      // Track pointer and keyboard interactions while open.
      on(openCleanups, doc, "pointerdown", (event) => {
        const target = getTarget(event);
        const insideElements = getResolvedInsideElements();
        const pointerTargetInside = t().contains(floating, target) || t().contains(domReference, target) ||
          t().contains(portalNode, target) || insideElements.some((element) => element === target || t().contains(element, target));
        pointerDownOutside = !pointerTargetInside;
        lastInteractionType = event.pointerType || "keyboard";
        if (target?.closest?.(`[${CLICK_TRIGGER_IDENTIFIER}]`)) {
          isPointerDown = true;
          clearTimeout(pointerDownTimer);
          pointerDownTimer = setTimeout(() => {
            isPointerDown = false;
          }, 0);
        }
      }, true);
      const clearPointerDownOutside = () => {
        pointerDownOutside = false;
      };
      on(openCleanups, doc, "pointerup", clearPointerDownOutside, true);
      on(openCleanups, doc, "pointercancel", clearPointerDownOutside, true);
      on(openCleanups, doc, "keydown", () => {
        lastInteractionType = "keyboard";
      }, true);
      openCleanups.push(clearPointerDownOutside);

      // Hide everything outside the floating tree from assistive tech while open.
      const nestedPortalNodes = Array.from(portalNode?.querySelectorAll("[data-base-ui-portal]") || []);
      const portalGuards = portalNode?._templPortalGuards;
      const insideElements = [
        floating, ...nestedPortalNodes, beforeGuard, afterGuard, portalGuards?.beforeOutside, portalGuards?.afterOutside,
        ...getResolvedInsideElements(), previousFocusableElement, nextFocusableElement,
        isUntrappedTypeableCombobox ? domReference : null,
      ].filter((x) => x != null);
      const ariaHiddenCleanup = window.templ.markOthers(insideElements, { ariaHidden: modal || isUntrappedTypeableCombobox, mark: false });
      const markerCleanup = window.templ.markOthers([floating, ...nestedPortalNodes].filter((x) => x != null));
      openCleanups.push(() => {
        markerCleanup();
        ariaHiddenCleanup();
      });

      // Focus the initial element.
      const previouslyFocusedElement = t().activeElement(doc);
      queueMicrotask(() => {
        const resolvedInitialFocus = typeof initialFocus === "function" ? initialFocus(openInteractionType || "") : initialFocus;
        if (resolvedInitialFocus === undefined || resolvedInitialFocus === false) return;
        if (t().contains(floatingFocusElement, previouslyFocusedElement)) return;
        const getDefaultFocusElement = () => getTabbableContent(floatingFocusElement)[0] || floatingFocusElement;
        let elToFocus = resolvedInitialFocus === true || resolvedInitialFocus === null ? getDefaultFocusElement() : resolvedInitialFocus;
        elToFocus = elToFocus || getDefaultFocusElement();
        const hadFocusInside = t().contains(floatingFocusElement, t().activeElement(doc));
        enqueueFocus(elToFocus, {
          preventScroll: elToFocus === floatingFocusElement,
          shouldFocus() {
            if (!self.open) return false;
            if (hadFocusInside) return true;
            const currentActiveElement = t().activeElement(doc);
            return !(currentActiveElement !== elToFocus && t().contains(floatingFocusElement, currentActiveElement));
          },
        });
      });
    }

    // Return focus targets, restored on unmount. Only a null interaction
    // type is a programmatic open.
    const elementFocusedBeforeOpen = t().activeElement(doc);
    const preferPreviousFocus = openInteractionType == null;
    addPreviouslyFocusedElement(elementFocusedBeforeOpen);

    function getReturnElement() {
      let resolved = typeof returnFocus === "function" ? returnFocus(closeType) : returnFocus;
      if (resolved === undefined || resolved === false) return null;
      if (resolved === null) resolved = true;
      const referenceReturnElement = domReference?.isConnected ? domReference : null;
      const previousReturnElement = elementFocusedBeforeOpen?.isConnected && elementFocusedBeforeOpen.nodeName.toLowerCase() !== "body"
        ? elementFocusedBeforeOpen
        : null;
      let defaultReturnElement = preferPreviousFocus
        ? previousReturnElement || referenceReturnElement
        : referenceReturnElement || previousReturnElement;
      if (!defaultReturnElement) defaultReturnElement = getPreviouslyFocusedElement() || null;
      if (typeof resolved === "boolean") return defaultReturnElement;
      return resolved || defaultReturnElement || null;
    }

    // The portal renders its outside guards for a non modal manager.
    window.templ.portal.setFocusManagerState(portalNode, {
      modal,
      closeOnFocusOut,
      open: false,
      onOpenChange,
      domReference,
      beforeInside: beforeGuard,
      afterInside: afterGuard,
    });
    if (floatingFocusElement) handleTabIndex(floatingFocusElement);
    instances.add(self);
    open();

    function close(details = {}) {
      if (!self.open) return;
      self.open = false;
      closeType = getEventType(details.event, lastInteractionType);
      if (details.reason === "trigger-hover" && details.event?.type === "mouseleave") preventReturnFocus = true;
      if (details.reason === "outside-press") {
        // An outside press returns focus without scrolling where the browser
        // supports preventScroll, everywhere current.
        const event = details.event;
        if (details.nested || (event && (isVirtualClick(event) || isVirtualPointerEvent(event)))) {
          preventReturnFocus = false;
        } else {
          let isPreventScrollSupported = false;
          doc.createElement("div").focus({
            get preventScroll() {
              isPreventScrollSupported = true;
              return false;
            },
          });
          preventReturnFocus = !isPreventScrollSupported;
        }
      }
      openCleanups.splice(0).forEach((cleanup) => cleanup());
      window.templ.portal.setFocusManagerState(portalNode, { ...portalNode?._templFocusState, open: false });
      // Safari may scroll to the bottom when an input inside the popup keeps
      // focus while it unmounts.
      const activeEl = t().activeElement(doc);
      if (webkit && isHTMLElement(activeEl) && isTypeableElement(activeEl) && t().contains(floating, activeEl)) activeEl.blur();
    }

    function unmount() {
      if (self.open) close();
      instances.delete(self);
      cleanups.splice(0).forEach((cleanup) => cleanup());
      clearTimeout(blurTimer);
      clearTimeout(pointerDownTimer);
      window.templ.portal.setFocusManagerState(portalNode, null);
      beforeGuard?.remove();
      afterGuard?.remove();

      const activeEl = t().activeElement(doc);
      const isFocusInsideFloatingTree = t().contains(floating, activeEl) ||
        getResolvedInsideElements().some((element) => element === activeEl || t().contains(element, activeEl)) ||
        [...instances].some((other) => isChild(other) && t().contains(other.floating, activeEl));
      const returnElement = getReturnElement();
      queueMicrotask(() => {
        const tabbableReturnElement = getFirstTabbableElement(returnElement);
        const hasExplicitReturnFocus = typeof returnFocus !== "boolean";
        // Focus that moved elsewhere after mount is respected.
        if (returnFocus && !preventReturnFocus && isHTMLElement(tabbableReturnElement) &&
          (!hasExplicitReturnFocus && tabbableReturnElement !== activeEl && activeEl !== doc.body ? isFocusInsideFloatingTree : true)) {
          tabbableReturnElement.focus({ preventScroll: true });
        }
        preventReturnFocus = false;
        clearDisconnectedPreviouslyFocusedElements();
      });
    }

    // beforeGuard is the source's beforeContentFocusGuardRef.
    return { open, close, unmount, beforeGuard };
  }

  window.templ = window.templ || {};
  window.templ.focusManager = { useFloatingFocusManager, createFocusGuard, enqueueFocus };
})();

// components/baseui/lifecycle.js
// Pendant of React mount and unmount. A Base UI part mounts when React renders
// it and unmounts when React removes it, and a portaled part stays mounted as
// long as the element that rendered it. Here one MutationObserver does that
// for every component: init(el) runs once when an element matching the
// selector appears, destroy(el) once when it is gone.
(function () {
  "use strict";

  const components = [];

  // A portaled subtree lives as long as its declaration site (portal owner),
  // like React unmounts a portal with the component that rendered it.
  function mounted(el) {
    if (!el.isConnected) return false;
    for (let node = el; node; node = node.parentElement) {
      if (node._templPortalOwner && !mounted(node._templPortalOwner)) return false;
    }
    return true;
  }

  function sync(component) {
    for (const el of component.elements) {
      if (mounted(el)) continue;
      component.elements.delete(el);
      component.destroy?.(el);
    }
    for (const el of document.querySelectorAll(component.selector)) {
      if (component.elements.has(el) || !mounted(el)) continue;
      component.elements.add(el);
      component.init?.(el);
    }
  }

  // register(selector, { init, destroy }) wires every matching element that
  // is in the document now and every one that appears later.
  function register(selector, { init, destroy } = {}) {
    const component = { selector, init, destroy, elements: new Set() };
    components.push(component);
    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", () => sync(component));
    else sync(component);
  }

  new MutationObserver(() => components.forEach(sync))
    .observe(document.documentElement, { childList: true, subtree: true });

  window.templ = window.templ || {};
  window.templ.lifecycle = { register };
})();

// components/baseui/mark_others.js
// Port of @base-ui/react floating-ui-react/utils/markOthers.ts (1.6.0), itself a
// fork of aria-hidden with a conditional aria-hidden. markOthers(avoid, options)
// marks every element outside the avoided subtrees and returns the undo.
//
//   ariaHidden  sets aria-hidden="true" outside (modal popups)
//   inert       sets inert outside
//   mark        sets the data-base-ui-inert marker outside, which useDismiss
//               reads to tell elements injected after opening (default true)
//
// Counted per element, so nested popups and several open ones undo cleanly,
// and elements that were hidden before are left as they were.
(function () {
  "use strict";

  const markerName = "data-base-ui-inert";
  let counters = { inert: new WeakMap(), "aria-hidden": new WeakMap() };
  let uncontrolledElementsSets = { inert: new WeakSet(), "aria-hidden": new WeakSet() };
  let markerCounterMap = new WeakMap();
  let lockCount = 0;

  const isShadowRoot = (node) => typeof ShadowRoot !== "undefined" && node instanceof ShadowRoot;

  function unwrapHost(node) {
    if (!node) return null;
    return isShadowRoot(node) ? node.host : unwrapHost(node.parentNode);
  }

  function correctElements(parent, targets) {
    return targets.map((target) => {
      if (parent.contains(target)) return target;
      const correctedTarget = unwrapHost(target);
      if (parent.contains(correctedTarget)) return correctedTarget;
      return null;
    }).filter((x) => x != null);
  }

  function buildKeepSet(targets) {
    const keep = new Set();
    targets.forEach((target) => {
      let node = target;
      while (node && !keep.has(node)) {
        keep.add(node);
        node = node.parentNode;
      }
    });
    return keep;
  }

  function collectOutsideElements(root, keepElements, stopElements) {
    const outside = [];
    const walk = (parent) => {
      if (!parent || stopElements.has(parent)) return;
      Array.from(parent.children).forEach((node) => {
        if (node.nodeName.toLowerCase() === "script") return;
        if (keepElements.has(node)) walk(node);
        else outside.push(node);
      });
    };
    walk(root);
    return outside;
  }

  function applyAttributeToOthers(uncorrectedAvoidElements, body, ariaHidden, inert, { mark = true }) {
    let controlAttribute = null;
    if (inert) controlAttribute = "inert";
    else if (ariaHidden) controlAttribute = "aria-hidden";
    let counterMap = null;
    let uncontrolledElementsSet = null;
    const avoidElements = correctElements(body, uncorrectedAvoidElements);
    const markerTargets = mark ? collectOutsideElements(body, buildKeepSet(avoidElements), new Set(avoidElements)) : [];
    const hiddenElements = [];
    const markedElements = [];
    if (controlAttribute) {
      const map = counters[controlAttribute];
      uncontrolledElementsSet = uncontrolledElementsSets[controlAttribute];
      counterMap = map;
      const ariaLiveElements = correctElements(body, Array.from(body.querySelectorAll("[aria-live]")));
      const controlElements = avoidElements.concat(ariaLiveElements);
      const controlTargets = collectOutsideElements(body, buildKeepSet(controlElements), new Set(controlElements));
      controlTargets.forEach((node) => {
        const attr = node.getAttribute(controlAttribute);
        const alreadyHidden = attr !== null && attr !== "false";
        const counterValue = (map.get(node) || 0) + 1;
        map.set(node, counterValue);
        hiddenElements.push(node);
        if (counterValue === 1 && alreadyHidden) uncontrolledElementsSet.add(node);
        if (!alreadyHidden) node.setAttribute(controlAttribute, controlAttribute === "inert" ? "" : "true");
      });
    }
    if (mark) {
      markerTargets.forEach((node) => {
        const markerValue = (markerCounterMap.get(node) || 0) + 1;
        markerCounterMap.set(node, markerValue);
        markedElements.push(node);
        if (markerValue === 1) node.setAttribute(markerName, "");
      });
    }
    lockCount += 1;
    return () => {
      if (counterMap) {
        hiddenElements.forEach((element) => {
          const counterValue = (counterMap.get(element) || 0) - 1;
          counterMap.set(element, counterValue);
          if (!counterValue) {
            if (!uncontrolledElementsSet?.has(element) && controlAttribute) element.removeAttribute(controlAttribute);
            uncontrolledElementsSet?.delete(element);
          }
        });
      }
      if (mark) {
        markedElements.forEach((element) => {
          const markerValue = (markerCounterMap.get(element) || 0) - 1;
          markerCounterMap.set(element, markerValue);
          if (!markerValue) element.removeAttribute(markerName);
        });
      }
      lockCount -= 1;
      if (!lockCount) {
        counters = { inert: new WeakMap(), "aria-hidden": new WeakMap() };
        uncontrolledElementsSets = { inert: new WeakSet(), "aria-hidden": new WeakSet() };
        markerCounterMap = new WeakMap();
      }
    };
  }

  function markOthers(avoidElements, options = {}) {
    const { ariaHidden = false, inert = false, mark = true } = options;
    const body = avoidElements[0].ownerDocument.body;
    return applyAttributeToOthers(avoidElements, body, ariaHidden, inert, { mark });
  }

  window.templ = window.templ || {};
  window.templ.markOthers = markOthers;
})();

// components/baseui/portal.js
// Port of @base-ui/react floating-ui-react/components/FloatingPortal.tsx (1.6.0).
//
// render(node) moves a [data-base-ui-portal] node to its container while its
// component stays where it was declared. That declaration site is the portal
// owner, and lifecycle.js unmounts the portaled subtree once the owner leaves
// the document. The container is the portal node of an enclosing portal, or
// <body> (useFloatingPortalNode). remove(node) unmounts it with its guards.
//
// setFocusManagerState(node, state) is the source's portal context: a non
// modal focus manager inside the node gets the outside guards at the
// declaration site, which move Tab between the trigger and the portaled
// content, and the node's tabbables are taken out of the tab order while
// focus is outside of it.
(function () {
  "use strict";

  let uid = 0;
  const t = () => window.templ.tabbable;

  // The React tree pendant: the parent of a portal node is where it was
  // declared, of every other node its DOM parent.
  function treeParent(node) {
    return node._templPortalOwner || node.parentNode;
  }

  function containerFor(node) {
    return node._templPortalOwner?.closest("[data-base-ui-portal]") || document.body;
  }

  // Appends on every open, so paint order follows open order like the
  // portal nodes React creates on mount.
  function render(node) {
    if (!node._templPortalOwner) node._templPortalOwner = node.parentElement;
    if (!node.id) node.id = "templ-portal-" + ++uid;
    containerFor(node).appendChild(node);
  }

  function remove(node) {
    if (!node) return;
    setFocusManagerState(node, null);
    if (node.isConnected) node.remove();
  }

  // Tabbables inside the node count only once focus entered it, through an
  // outside guard or a pointer.
  function onFocus(event) {
    const node = event.currentTarget;
    if (!event.relatedTarget || !t().isOutsideEvent(event)) return;
    if (event.type === "focusin") {
      if (node._templFocusInsideDisabled) {
        t().enableFocusInside(node);
        node._templFocusInsideDisabled = false;
      }
    } else {
      t().disableFocusInside(node);
      node._templFocusInsideDisabled = true;
    }
  }

  function createOutsideGuards(node) {
    const { createFocusGuard } = window.templ.focusManager;
    const state = () => node._templFocusState;
    const beforeOutside = createFocusGuard("outside", (event) => {
      if (t().isOutsideEvent(event, node)) {
        state()?.beforeInside?.focus();
      } else {
        const domReference = state()?.domReference;
        if (domReference) t().getPreviousTabbable(domReference)?.focus();
      }
    });
    const afterOutside = createFocusGuard("outside", (event) => {
      if (t().isOutsideEvent(event, node)) {
        state()?.afterInside?.focus();
      } else {
        const domReference = state()?.domReference;
        if (domReference) t().getNextTabbable(domReference)?.focus();
        if (state()?.closeOnFocusOut) state()?.onOpenChange?.(false, "focus-out", event);
      }
    });
    const owns = document.createElement("span");
    owns.setAttribute("aria-owns", node.id);
    owns.style.cssText = "clip-path:inset(50%);position:fixed;top:0;left:0";
    node._templPortalOwner?.before(beforeOutside, owns, afterOutside);
    return { beforeOutside, afterOutside, owns };
  }

  function setFocusManagerState(node, state) {
    if (!node) return;
    node._templFocusState = state;
    const nonModal = !!state && !state.modal;

    if (nonModal && !node._templFocusListening) {
      node.addEventListener("focusin", onFocus, true);
      node.addEventListener("focusout", onFocus, true);
      node._templFocusListening = true;
    } else if (!nonModal && node._templFocusListening) {
      node.removeEventListener("focusin", onFocus, true);
      node.removeEventListener("focusout", onFocus, true);
      node._templFocusListening = false;
    }
    // Tabbable again before the focus manager's queued initial focus runs.
    if (state?.open && node._templFocusInsideDisabled) {
      t().enableFocusInside(node);
      node._templFocusInsideDisabled = false;
    }

    const shouldRenderGuards = nonModal && state.open;
    if (shouldRenderGuards && !node._templPortalGuards) {
      node._templPortalGuards = createOutsideGuards(node);
    } else if (!shouldRenderGuards && node._templPortalGuards) {
      const { beforeOutside, afterOutside, owns } = node._templPortalGuards;
      beforeOutside.remove();
      owns.remove();
      afterOutside.remove();
      node._templPortalGuards = null;
    }
  }

  window.templ = window.templ || {};
  window.templ.portal = { render, remove, setFocusManagerState, treeParent };
})();

// components/baseui/scroll_lock.js
// Port of @base-ui/utils/useScrollLock.ts. Helpers from useTimeout.ts,
// useAnimationFrame.ts, platform/os.ts, platform/engine.ts, @floating-ui/utils/dom,
// and @base-ui/react/utils/useAnchoredPopupScrollLock.ts live here as well.
(function () {
  "use strict";

  // @base-ui/utils/platform/{os,engine}.ts
  const lowerPlatform = navigator.platform.toLowerCase();
  const ios = /^i(os$|p)/.test(lowerPlatform) ||
    (lowerPlatform === "macintel" && navigator.maxTouchPoints > 1);
  const webkit = typeof CSS !== "undefined" && !!CSS.supports?.("-webkit-backdrop-filter:none");

  const ownerDocument = (referenceElement) => referenceElement?.ownerDocument || document;
  const ownerWindow = (referenceElement) =>
    (referenceElement?.nodeType === 9 ? referenceElement : ownerDocument(referenceElement)).defaultView || window;

  // @floating-ui/utils/dom: isOverflowElement
  function isOverflowElement(element) {
    const { overflow, overflowX, overflowY, display } = ownerWindow(element).getComputedStyle(element);
    return /auto|scroll|overlay|hidden|clip/.test(overflow + overflowY + overflowX) &&
      display !== "inline" && display !== "contents";
  }

  // @base-ui/utils/useTimeout.ts (the imperative helper; no React lifecycle).
  class Timeout {
    static create() { return new Timeout(); }
    currentId = 0;
    start(delay, fn) {
      this.clear();
      this.currentId = setTimeout(() => {
        this.currentId = 0;
        fn();
      }, delay);
    }
    isStarted() { return this.currentId !== 0; }
    clear = () => {
      if (this.currentId !== 0) {
        clearTimeout(this.currentId);
        this.currentId = 0;
      }
    };
  }

  // @base-ui/utils/useAnimationFrame.ts, including its production scheduler.
  class Scheduler {
    callbacks = [];
    callbacksCount = 0;
    nextId = 1;
    startId = 1;
    isScheduled = false;
    tick = (timestamp) => {
      this.isScheduled = false;
      const currentCallbacks = this.callbacks;
      const currentCallbacksCount = this.callbacksCount;
      this.callbacks = [];
      this.callbacksCount = 0;
      this.startId = this.nextId;
      if (currentCallbacksCount > 0) {
        for (let i = 0; i < currentCallbacks.length; i += 1) {
          currentCallbacks[i]?.(timestamp);
        }
      }
    };
    request(fn) {
      const id = this.nextId;
      this.nextId += 1;
      this.callbacks.push(fn);
      this.callbacksCount += 1;
      if (!this.isScheduled) {
        requestAnimationFrame(this.tick);
        this.isScheduled = true;
      }
      return id;
    }
    cancel(id) {
      const index = id - this.startId;
      if (index < 0 || index >= this.callbacks.length || this.callbacks[index] === null) return;
      this.callbacks[index] = null;
      this.callbacksCount -= 1;
    }
  }
  const scheduler = new Scheduler();
  class AnimationFrame {
    static create() { return new AnimationFrame(); }
    static request(fn) { return scheduler.request(fn); }
    static cancel(id) { scheduler.cancel(id); }
    currentId = null;
    request(fn) {
      this.cancel();
      this.currentId = scheduler.request(() => {
        this.currentId = null;
        fn();
      });
    }
    cancel = () => {
      if (this.currentId !== null) {
        scheduler.cancel(this.currentId);
        this.currentId = null;
      }
    };
  }

  let originalHtmlStyles = {};
  let originalBodyStyles = {};
  let originalHtmlScrollBehavior = '';

  // The viewport's overflow comes from <html> when it establishes its own scroll container, and
  // propagates from <body> otherwise. An `overflow` style on the other element doesn't lock the page.
  function getViewportScroller(html, body) {
    return isOverflowElement(html) ? html : body;
  }

  function isPageScrollLocked(win, html, body) {
    return /hidden|clip/.test(win.getComputedStyle(getViewportScroller(html, body)).overflowY);
  }

  function hasInsetScrollbars(referenceElement) {
    if (typeof document === 'undefined') {
      return false;
    }
    const doc = ownerDocument(referenceElement);
    const win = ownerWindow(doc);
    return win.innerWidth - doc.documentElement.clientWidth > 0;
  }

  function supportsStableScrollbarGutter(referenceElement) {
    const supported =
      typeof CSS !== 'undefined' && CSS.supports && CSS.supports('scrollbar-gutter', 'stable');

    if (!supported || typeof document === 'undefined') {
      return false;
    }

    const doc = ownerDocument(referenceElement);
    const html = doc.documentElement;
    const body = doc.body;

    const scrollContainer = getViewportScroller(html, body);

    const originalScrollContainerOverflowY = scrollContainer.style.overflowY;
    const originalHtmlStyleGutter = html.style.scrollbarGutter;

    html.style.scrollbarGutter = 'stable';

    scrollContainer.style.overflowY = 'scroll';
    const before = scrollContainer.offsetWidth;

    scrollContainer.style.overflowY = 'hidden';
    const after = scrollContainer.offsetWidth;

    scrollContainer.style.overflowY = originalScrollContainerOverflowY;
    html.style.scrollbarGutter = originalHtmlStyleGutter;

    return before === after;
  }

  function preventScrollOverlayScrollbars(referenceElement) {
    const doc = ownerDocument(referenceElement);
    const html = doc.documentElement;
    const body = doc.body;

    // If an `overflow` style is present on <html>, we need to lock it, because a lock on <body>
    // won't have any effect.
    // But if <body> has an `overflow` style (like `overflow-x: hidden`), we need to lock it
    // instead, as sticky elements shift otherwise.
    const elementToLock = getViewportScroller(html, body);
    const originalElementToLockStyles = {
      overflowY: elementToLock.style.overflowY,
      overflowX: elementToLock.style.overflowX,
    };

    Object.assign(elementToLock.style, {
      overflowY: 'hidden',
      overflowX: 'hidden',
    });

    return () => {
      Object.assign(elementToLock.style, originalElementToLockStyles);
    };
  }

  function preventScrollInsetScrollbars(referenceElement) {
    const doc = ownerDocument(referenceElement);
    const html = doc.documentElement;
    const body = doc.body;
    const win = ownerWindow(html);

    let scrollTop = 0;
    let scrollLeft = 0;
    let updateGutterOnly = false;
    const resizeFrame = AnimationFrame.create();

    // Pinch-zoom in Safari causes a shift. Just don't lock scroll if there's any pinch-zoom.
    if (webkit && (win.visualViewport?.scale ?? 1) !== 1) {
      return () => {};
    }

    function lockScroll() {
      /* DOM reads: */

      const htmlStyles = win.getComputedStyle(html);
      const bodyStyles = win.getComputedStyle(body);
      const htmlScrollbarGutterValue = htmlStyles.scrollbarGutter || '';
      const hasBothEdges = htmlScrollbarGutterValue.includes('both-edges');
      const scrollbarGutterValue = hasBothEdges ? 'stable both-edges' : 'stable';

      scrollTop = html.scrollTop;
      scrollLeft = html.scrollLeft;

      originalHtmlStyles = {
        scrollbarGutter: html.style.scrollbarGutter,
        overflowY: html.style.overflowY,
        overflowX: html.style.overflowX,
      };
      originalHtmlScrollBehavior = html.style.scrollBehavior;

      originalBodyStyles = {
        position: body.style.position,
        height: body.style.height,
        width: body.style.width,
        boxSizing: body.style.boxSizing,
        overflowY: body.style.overflowY,
        overflowX: body.style.overflowX,
        scrollBehavior: body.style.scrollBehavior,
      };

      const isScrollableY = html.scrollHeight > html.clientHeight;
      const isScrollableX = html.scrollWidth > html.clientWidth;
      const hasConstantOverflowY =
        htmlStyles.overflowY === 'scroll' || bodyStyles.overflowY === 'scroll';
      const hasConstantOverflowX =
        htmlStyles.overflowX === 'scroll' || bodyStyles.overflowX === 'scroll';

      // Values can be negative in Firefox
      const scrollbarWidth = Math.max(0, win.innerWidth - body.clientWidth);
      const scrollbarHeight = Math.max(0, win.innerHeight - body.clientHeight);

      // Avoid shift due to the default <body> margin. This does cause elements to be clipped
      // with whitespace. Warn if <body> has margins?
      const marginY = parseFloat(bodyStyles.marginTop) + parseFloat(bodyStyles.marginBottom);
      const marginX = parseFloat(bodyStyles.marginLeft) + parseFloat(bodyStyles.marginRight);
      const elementToLock = getViewportScroller(html, body);

      updateGutterOnly = supportsStableScrollbarGutter(referenceElement);

      /*
       * DOM writes:
       * Do not read the DOM past this point!
       */

      if (updateGutterOnly) {
        html.style.scrollbarGutter = scrollbarGutterValue;
        elementToLock.style.overflowY = 'hidden';
        elementToLock.style.overflowX = 'hidden';
        return;
      }

      Object.assign(html.style, {
        scrollbarGutter: scrollbarGutterValue,
        overflowY: 'hidden',
        overflowX: 'hidden',
      });

      if (isScrollableY || hasConstantOverflowY) {
        html.style.overflowY = 'scroll';
      }
      if (isScrollableX || hasConstantOverflowX) {
        html.style.overflowX = 'scroll';
      }

      Object.assign(body.style, {
        position: 'relative',
        height:
          marginY || scrollbarHeight ? `calc(100dvh - ${marginY + scrollbarHeight}px)` : '100dvh',
        width: marginX || scrollbarWidth ? `calc(100vw - ${marginX + scrollbarWidth}px)` : '100vw',
        boxSizing: 'border-box',
        // Assign the longhands that `cleanup` restores, so nothing is left behind.
        overflowY: 'hidden',
        overflowX: 'hidden',
        scrollBehavior: 'unset',
      });

      body.scrollTop = scrollTop;
      body.scrollLeft = scrollLeft;
      html.setAttribute('data-base-ui-scroll-locked', '');
      html.style.scrollBehavior = 'unset';
    }

    function cleanup() {
      Object.assign(html.style, originalHtmlStyles);
      Object.assign(body.style, originalBodyStyles);

      if (!updateGutterOnly) {
        html.scrollTop = scrollTop;
        html.scrollLeft = scrollLeft;
        html.removeAttribute('data-base-ui-scroll-locked');
        html.style.scrollBehavior = originalHtmlScrollBehavior;
      }
    }

    function handleResize() {
      cleanup();
      resizeFrame.request(lockScroll);
    }

    lockScroll();
    win.addEventListener('resize', handleResize);

    return () => {
      resizeFrame.cancel();
      cleanup();
      // Sometimes this cleanup can run after test teardown because it is called
      // in a `setTimeout(fn, 0)`. Guard the returned cleanup to avoid calling
      // `removeEventListener` when it is no longer available in tests.
      if (typeof win.removeEventListener === 'function') {
        win.removeEventListener('resize', handleResize);
      }
    };
  }

  class ScrollLocker {
    lockCount = 0;
    restore = null;
    timeoutLock = Timeout.create();
    timeoutUnlock = Timeout.create();

    acquire(referenceElement) {
      this.lockCount += 1;
      if (this.lockCount === 1 && this.restore === null) {
        this.timeoutLock.start(0, () => this.lock(referenceElement));
      }
      return this.release;
    }

    release = () => {
      this.lockCount -= 1;
      if (this.lockCount === 0 && this.restore) {
        this.timeoutUnlock.start(0, this.unlock);
      }
    };

    unlock = () => {
      if (this.lockCount === 0 && this.restore) {
        this.restore?.();
        this.restore = null;
      }
    };

    lock(referenceElement) {
      if (this.lockCount === 0 || this.restore !== null) {
        return;
      }

      const doc = ownerDocument(referenceElement);
      const html = doc.documentElement;
      const body = doc.body;
      const win = ownerWindow(html);

      // The page is already locked, either by the site author or by a non-Base UI overlay that
      // hasn't cleaned up yet. Leave it alone and wait for the lock to clear before taking over,
      // otherwise we'd snapshot the locked state and restore it after our own lock is released.
      if (isPageScrollLocked(win, html, body)) {
        const observer = new win.MutationObserver(() => {
          if (isPageScrollLocked(win, html, body)) {
            return;
          }
          observer.disconnect();
          this.restore = null;
          this.lock(referenceElement);
        });

        // Watch every attribute: locks are applied through inline styles, classes, or attributes
        // paired with a stylesheet (`data-scroll-locked` in react-remove-scroll, for example).
        const options = { attributes: true };

        observer.observe(html, options);
        observer.observe(body, options);

        this.restore = () => observer.disconnect();
        return;
      }

      const hasOverlayScrollbars = ios || !hasInsetScrollbars(referenceElement);

      // On iOS, scroll locking does not work if the navbar is collapsed. Due to numerous
      // side effects and bugs that arise on iOS, it must be researched extensively before
      // being enabled to ensure it doesn't cause the following issues:
      // - Textboxes must scroll into view when focused, nor cause a glitchy scroll animation.
      // - The navbar must not force itself into view and cause layout shift.
      // - Scroll containers must not flicker upon closing a popup when it has an exit animation.
      this.restore = hasOverlayScrollbars
        ? preventScrollOverlayScrollbars(referenceElement)
        : preventScrollInsetScrollbars(referenceElement);
    }
  }

  const SCROLL_LOCKER = new ScrollLocker();

  // @base-ui/react/utils/useAnchoredPopupScrollLock.ts: run after positioning.
  const VIEWPORT_WIDTH_TOLERANCE_PX = 20;
  function anchoredPopupScrollLock(enabled, touchOpen, positionerElement, referenceElement) {
    let touchOpenShouldLockScroll = false;
    if (enabled && touchOpen && positionerElement != null) {
      const viewportWidth = ownerDocument(positionerElement).documentElement.clientWidth;
      const popupWidth = positionerElement.offsetWidth;
      touchOpenShouldLockScroll = viewportWidth > 0 && popupWidth > 0 &&
        popupWidth >= viewportWidth - VIEWPORT_WIDTH_TOLERANCE_PX;
    }
    return enabled && (!touchOpen || touchOpenShouldLockScroll)
      ? SCROLL_LOCKER.acquire(referenceElement)
      : () => {};
  }

  window.templ = window.templ || {};
  window.templ.scrollLock = {
    acquire: (referenceElement) => SCROLL_LOCKER.acquire(referenceElement),
    anchoredPopup: anchoredPopupScrollLock,
  };
})();

// components/baseui/tabbable.js
// Port of @base-ui/react floating-ui-react/utils/tabbable.ts (1.6.0), Base UI's
// own tabbable implementation, with activeElement and contains from
// utils/element.ts and isElementVisible from utils/composite.ts.
(function () {
  "use strict";

  const CANDIDATE_SELECTOR = 'a[href],button,input,select,textarea,summary,details,iframe,object,embed,[tabindex],[contenteditable]:not([contenteditable="false"]),audio[controls],video[controls]';

  const nodeName = (element) => (element?.nodeName || "").toLowerCase();
  const isShadowRoot = (node) => typeof ShadowRoot !== "undefined" && node instanceof ShadowRoot;
  const isHTMLElement = (node) => node instanceof HTMLElement;

  // The focused element, through open shadow roots.
  function activeElement(doc) {
    let element = doc.activeElement;
    while (element?.shadowRoot?.activeElement != null) element = element.shadowRoot.activeElement;
    return element;
  }

  function contains(parent, child) {
    if (!parent || !child) return false;
    const rootNode = child.getRootNode?.();
    if (parent.contains(child)) return true;
    if (rootNode && isShadowRoot(rootNode)) {
      let next = child;
      while (next) {
        if (parent === next) return true;
        next = next.parentNode || next.host;
      }
    }
    return false;
  }

  function isHiddenByStyles(styles) {
    return styles.visibility === "hidden" || styles.visibility === "collapse";
  }

  function isElementVisible(element, styles = element ? getComputedStyle(element) : null) {
    if (!element || !element.isConnected || !styles || isHiddenByStyles(styles)) return false;
    if (typeof element.checkVisibility === "function") return element.checkVisibility();
    return styles.display !== "none" && styles.display !== "contents";
  }

  function getParentElement(element) {
    if (element.assignedSlot) return element.assignedSlot;
    if (element.parentElement) return element.parentElement;
    const rootNode = element.getRootNode();
    return isShadowRoot(rootNode) ? rootNode.host : null;
  }

  function getDetailsSummary(details) {
    for (const child of Array.from(details.children)) {
      if (nodeName(child) === "summary") return child;
    }
    return null;
  }

  function isWithinOpenDetailsSummary(element, details) {
    const summary = getDetailsSummary(details);
    return !!summary && (element === summary || contains(summary, element));
  }

  function isFocusableCandidate(element) {
    const name = element ? nodeName(element) : "";
    return element != null && element.matches(CANDIDATE_SELECTOR) &&
      (name !== "summary" || (element.parentElement != null && nodeName(element.parentElement) === "details" && getDetailsSummary(element.parentElement) === element)) &&
      (name !== "details" || getDetailsSummary(element) == null) &&
      (name !== "input" || element.type !== "hidden");
  }

  function isVisibleInTabbableTree(element, isAncestor) {
    const styles = getComputedStyle(element);
    if (!isAncestor) return isElementVisible(element, styles);
    return styles.display !== "none";
  }

  function isFocusableElement(element) {
    if (!isFocusableCandidate(element) || !element.isConnected || element.matches(":disabled")) return false;
    for (let current = element; current; current = getParentElement(current)) {
      const isAncestor = current !== element;
      const isSlot = nodeName(current) === "slot";
      if (current.hasAttribute("inert")) return false;
      if ((isAncestor && nodeName(current) === "details" && !current.open && !isWithinOpenDetailsSummary(element, current)) ||
        current.hasAttribute("hidden") || (!isSlot && !isVisibleInTabbableTree(current, isAncestor))) {
        return false;
      }
    }
    return true;
  }

  function getTabIndex(element) {
    const tabIndex = element.tabIndex;
    if (tabIndex < 0) {
      const name = nodeName(element);
      if (name === "details" || name === "audio" || name === "video" || (isHTMLElement(element) && element.isContentEditable)) return 0;
    }
    return tabIndex;
  }

  function getNamedRadioInput(element) {
    if (nodeName(element) !== "input") return null;
    return element.type === "radio" && element.name !== "" ? element : null;
  }

  function isTabbableRadio(element, candidates) {
    const input = getNamedRadioInput(element);
    if (!input) return true;
    const checkedRadio = candidates.find((candidate) => {
      const radio = getNamedRadioInput(candidate);
      return radio?.name === input.name && radio.form === input.form && radio.checked;
    });
    if (checkedRadio) return checkedRadio === input;
    return candidates.find((candidate) => {
      const radio = getNamedRadioInput(candidate);
      return radio?.name === input.name && radio.form === input.form;
    }) === input;
  }

  function getComposedChildren(container) {
    if (isHTMLElement(container) && nodeName(container) === "slot") {
      const assignedElements = container.assignedElements({ flatten: true });
      if (assignedElements.length > 0) return assignedElements;
    }
    if (isHTMLElement(container) && container.shadowRoot) return Array.from(container.shadowRoot.children);
    return Array.from(container.children);
  }

  function appendCandidates(container, list) {
    getComposedChildren(container).forEach((child) => {
      if (isFocusableCandidate(child)) list.push(child);
      appendCandidates(child, list);
    });
  }

  function appendMatchingElements(container, selector, list) {
    getComposedChildren(container).forEach((child) => {
      if (isHTMLElement(child) && child.matches(selector)) list.push(child);
      appendMatchingElements(child, selector, list);
    });
  }

  function isTabbable(element) {
    return isFocusableElement(element) && getTabIndex(element) >= 0;
  }

  function focusable(container) {
    const candidates = [];
    appendCandidates(container, candidates);
    return candidates.filter(isFocusableElement);
  }

  function tabbable(container) {
    const candidates = focusable(container);
    return candidates.filter((element) => getTabIndex(element) >= 0 && isTabbableRadio(element, candidates));
  }

  function getTabbableIn(container, dir) {
    const list = tabbable(container);
    const len = list.length;
    if (len === 0) return undefined;
    const active = activeElement(container.ownerDocument);
    const index = list.indexOf(active);
    const nextIndex = index === -1 ? (dir === 1 ? 0 : len - 1) : index + dir;
    return list[nextIndex];
  }

  function getNextTabbable(referenceElement) {
    return getTabbableIn(referenceElement.ownerDocument.body, 1) || referenceElement;
  }

  function getPreviousTabbable(referenceElement) {
    return getTabbableIn(referenceElement.ownerDocument.body, -1) || referenceElement;
  }

  function getTabbableNearElement(referenceElement, dir) {
    if (!referenceElement) return null;
    const list = tabbable(referenceElement.ownerDocument.body);
    const elementCount = list.length;
    if (elementCount === 0) return null;
    const index = list.indexOf(referenceElement);
    if (index === -1) return null;
    return list[(index + dir + elementCount) % elementCount];
  }

  function getTabbableAfterElement(referenceElement) {
    return getTabbableNearElement(referenceElement, 1);
  }

  function getTabbableBeforeElement(referenceElement) {
    return getTabbableNearElement(referenceElement, -1);
  }

  function isOutsideEvent(event, container) {
    const containerElement = container || event.currentTarget;
    const relatedTarget = event.relatedTarget;
    return !relatedTarget || !contains(containerElement, relatedTarget);
  }

  // data-tabindex is Base UI's own marker for the tabindex it took away.
  function disableFocusInside(container) {
    tabbable(container).forEach((element) => {
      element.dataset.tabindex = element.getAttribute("tabindex") || "";
      element.setAttribute("tabindex", "-1");
    });
  }

  function enableFocusInside(container) {
    const elements = [];
    appendMatchingElements(container, "[data-tabindex]", elements);
    elements.forEach((element) => {
      const tabindex = element.dataset.tabindex;
      delete element.dataset.tabindex;
      if (tabindex) element.setAttribute("tabindex", tabindex);
      else element.removeAttribute("tabindex");
    });
  }

  window.templ = window.templ || {};
  window.templ.tabbable = {
    activeElement,
    contains,
    isElementVisible,
    isTabbable,
    focusable,
    tabbable,
    getNextTabbable,
    getPreviousTabbable,
    getTabbableAfterElement,
    getTabbableBeforeElement,
    isOutsideEvent,
    disableFocusInside,
    enableFocusInside,
  };
})();

// components/baseui/use_anchor_positioning.js
// Port of @base-ui/react utils/useAnchorPositioning.ts (1.6.0) over
// window.FloatingUIDOM from components/floatingui, with the floatingStyles of
// @floating-ui/react's useFloating.
//
// useAnchorPositioning(options) runs while the popup is mounted: it positions
// the positioner against the anchor and keeps it there (autoUpdate), and
// returns { cleanup, positioned }, where positioned resolves after the first
// position.
//
//   anchor              element, or a virtual element with getBoundingClientRect
//   positioner          the element that is positioned
//   parts               elements that render data-side and data-align (positioner, popup, arrow)
//   arrow               the arrow element, if any
//   positionMethod      "absolute" or "fixed"
//   side                top, right, bottom, left, inline-start, inline-end
//   align               start, center, end
//   sideOffset, alignOffset, collisionPadding, arrowPadding, sticky
//   collisionAvoidance  { side, align, fallbackAxisSide }
//   shiftCrossAxis, lazyFlip, disableAnchorTracking
//   inline              an extra middleware placed first, like the source
//   applyPosition       a function; while it returns false the positioner keeps
//                       its own position (select aligned with its trigger), the
//                       variables are still set
//   onPosition(result)  called after every position
//
// Left out: adaptiveOrigin, which only popups with a viewport part use.
(function () {
  "use strict";

  const getSide = (placement) => placement.split("-")[0];
  const getAlignment = (placement) => placement.split("-")[1];
  const getSideAxis = (side) => (side === "top" || side === "bottom" ? "y" : "x");

  function getLogicalSide(sideParam, renderedSide, isRtl) {
    const isLogicalSideParam = sideParam === "inline-start" || sideParam === "inline-end";
    const logicalRight = isRtl ? "inline-start" : "inline-end";
    const logicalLeft = isRtl ? "inline-end" : "inline-start";
    return {
      top: "top",
      right: isLogicalSideParam ? logicalRight : "right",
      bottom: "bottom",
      left: isLogicalSideParam ? logicalLeft : "left",
    }[renderedSide];
  }

  // DirectionProvider pendant: the nearest dir attribute.
  function isRtlAt(element) {
    const el = element?.closest ? element : element?.contextElement;
    return el?.closest?.("[dir]")?.getAttribute("dir") === "rtl";
  }

  // utils/hideMiddleware.ts: an anchor with an empty rect counts as hidden too.
  function hideMiddleware() {
    const nativeHideFn = window.FloatingUIDOM.hide().fn;
    return {
      name: "hide",
      async fn(state) {
        const { width, height, x, y } = state.rects.reference;
        const anchorHidden = width === 0 && height === 0 && x === 0 && y === 0;
        const nativeHideResult = await nativeHideFn(state);
        return { data: { referenceHidden: nativeHideResult.data?.referenceHidden || anchorHidden } };
      },
    };
  }

  // floating-ui-react/middleware/arrow.ts: Base UI's fork of arrow() that
  // measures against the positioner, not the arrow's offset parent.
  function arrowMiddleware({ element, padding = 0 }) {
    return {
      name: "arrow",
      async fn(state) {
        const { x, y, placement, rects, platform, elements, middlewareData } = state;
        const paddingObject = typeof padding === "number"
          ? { top: padding, right: padding, bottom: padding, left: padding }
          : { top: 0, right: 0, bottom: 0, left: 0, ...padding };
        const coords = { x, y };
        const axis = getSideAxis(getSide(placement)) === "y" ? "x" : "y";
        const length = axis === "y" ? "height" : "width";
        const arrowDimensions = await platform.getDimensions(element);
        const isYAxis = axis === "y";
        const minProp = isYAxis ? "top" : "left";
        const maxProp = isYAxis ? "bottom" : "right";
        const clientProp = isYAxis ? "clientHeight" : "clientWidth";
        const endDiff = rects.reference[length] + rects.reference[axis] - coords[axis] - rects.floating[length];
        const startDiff = coords[axis] - rects.reference[axis];
        const clientSize = elements.floating[clientProp] || rects.floating[length];
        const centerToReference = endDiff / 2 - startDiff / 2;
        // Padding large enough to push the arrow off center is reduced.
        const largestPossiblePadding = clientSize / 2 - arrowDimensions[length] / 2 - 1;
        const minPadding = Math.min(paddingObject[minProp], largestPossiblePadding);
        const maxPadding = Math.min(paddingObject[maxProp], largestPossiblePadding);
        const min = minPadding;
        const max = clientSize - arrowDimensions[length] - maxPadding;
        const center = clientSize / 2 - arrowDimensions[length] / 2 + centerToReference;
        const offset = Math.min(Math.max(min, center), max);
        // A reference too small for an aligned arrow moves the popup itself,
        // with one reset so shift() still runs.
        const shouldAddOffset = !middlewareData.arrow && getAlignment(placement) != null && center !== offset &&
          rects.reference[length] / 2 - (center < min ? minPadding : maxPadding) - arrowDimensions[length] / 2 < 0;
        const alignmentOffset = shouldAddOffset ? (center < min ? center - min : center - max) : 0;
        return {
          [axis]: coords[axis] + alignmentOffset,
          data: {
            [axis]: offset,
            centerOffset: center - offset - alignmentOffset,
            ...(shouldAddOffset && { alignmentOffset }),
          },
          reset: shouldAddOffset,
        };
      },
    };
  }

  // @floating-ui/react roundByDPR
  function roundByDPR(element, value) {
    const dpr = element.ownerDocument.defaultView.devicePixelRatio || 1;
    return Math.round(value * dpr) / dpr;
  }

  function useAnchorPositioning(options) {
    const {
      anchor,
      positioner,
      arrow: arrowEl = null,
      positionMethod = "absolute",
      side: sideParam = "bottom",
      sideOffset = 0,
      align = "center",
      alignOffset = 0,
      collisionPadding: collisionPaddingParam = 5,
      sticky = false,
      arrowPadding = 5,
      disableAnchorTracking = false,
      inline: inlineMiddleware,
      collisionAvoidance = {},
      shiftCrossAxis = false,
      lazyFlip = false,
      applyPosition = () => true,
      onPosition,
    } = options;
    const parts = (options.parts || [positioner]).filter(Boolean);
    const { computePosition, autoUpdate, offset, flip, shift, limitShift, size } = window.FloatingUIDOM;

    const collisionAvoidanceSide = collisionAvoidance.side || "flip";
    const collisionAvoidanceAlign = collisionAvoidance.align || "flip";
    const collisionAvoidanceFallbackAxisSide = collisionAvoidance.fallbackAxisSide || "end";
    const isRtl = isRtlAt(anchor);
    let mountSide = null;

    // Create a bias to the preferred side. On iOS, when the software keyboard
    // opens, the input is exactly centered and could flip to the top.
    const bias = 1;
    const biasTop = sideParam === "bottom" ? bias : 0;
    const biasBottom = sideParam === "top" ? bias : 0;
    const biasLeft = sideParam === "right" ? bias : 0;
    const biasRight = sideParam === "left" ? bias : 0;
    const padding = typeof collisionPaddingParam === "number"
      ? { top: collisionPaddingParam, right: collisionPaddingParam, bottom: collisionPaddingParam, left: collisionPaddingParam }
      : collisionPaddingParam;
    const collisionPadding = {
      top: (padding.top || 0) + biasTop,
      right: (padding.right || 0) + biasRight,
      bottom: (padding.bottom || 0) + biasBottom,
      left: (padding.left || 0) + biasLeft,
    };
    const commonCollisionProps = { boundary: "clippingAncestors", padding: collisionPadding };

    function getOffsetData(state) {
      return {
        side: getLogicalSide(sideParam, getSide(state.placement), isRtl),
        align: getAlignment(state.placement) || "center",
        anchor: { width: state.rects.reference.width, height: state.rects.reference.height },
        positioner: { width: state.rects.floating.width, height: state.rects.floating.height },
      };
    }
    const resolveOffset = (value, state) => (typeof value === "function" ? value(getOffsetData(state)) : value);

    const shiftDisabled = collisionAvoidanceAlign === "none" && collisionAvoidanceSide !== "shift";
    const crossAxisShiftEnabled = !shiftDisabled && (sticky || shiftCrossAxis || collisionAvoidanceSide === "shift");

    function middleware() {
      const list = [];
      if (inlineMiddleware) list.push(inlineMiddleware);
      list.push(offset((state) => {
        const sideAxis = resolveOffset(sideOffset, state);
        const alignAxis = resolveOffset(alignOffset, state);
        return { mainAxis: sideAxis, crossAxis: alignAxis, alignmentAxis: alignAxis };
      }));
      const flipMiddleware = collisionAvoidanceSide === "none" ? null : flip({
        ...commonCollisionProps,
        // The popup flips once it was limited by its --available-height and
        // resizes, since the size() padding is smaller than this one.
        padding: {
          top: collisionPadding.top + bias,
          right: collisionPadding.right + bias,
          bottom: collisionPadding.bottom + bias,
          left: collisionPadding.left + bias,
        },
        mainAxis: !shiftCrossAxis && collisionAvoidanceSide === "flip",
        crossAxis: collisionAvoidanceAlign === "flip" ? "alignment" : false,
        fallbackAxisSideDirection: collisionAvoidanceFallbackAxisSide,
      });
      const shiftMiddleware = shiftDisabled ? null : shift((data) => {
        const html = data.elements.floating.ownerDocument.documentElement;
        return {
          ...commonCollisionProps,
          // The layout viewport, so pinch zooming does not shift context menus.
          rootBoundary: shiftCrossAxis ? { x: 0, y: 0, width: html.clientWidth, height: html.clientHeight } : undefined,
          mainAxis: collisionAvoidanceAlign !== "none",
          crossAxis: crossAxisShiftEnabled,
          limiter: sticky || shiftCrossAxis ? undefined : limitShift((limitData) => {
            if (!arrowEl) return {};
            const { width, height } = arrowEl.getBoundingClientRect();
            const sideAxis = getSideAxis(getSide(limitData.placement));
            const arrowSize = sideAxis === "y" ? width : height;
            const offsetAmount = sideAxis === "y"
              ? collisionPadding.left + collisionPadding.right
              : collisionPadding.top + collisionPadding.bottom;
            return { offset: arrowSize / 2 + offsetAmount / 2 };
          }),
        };
      });
      // https://floating-ui.com/docs/flip#combining-with-shift
      if (collisionAvoidanceSide === "shift" || collisionAvoidanceAlign === "shift" || align === "center") {
        list.push(shiftMiddleware, flipMiddleware);
      } else {
        list.push(flipMiddleware, shiftMiddleware);
      }
      list.push(
        size({
          ...commonCollisionProps,
          apply({ elements: { floating }, availableWidth, availableHeight, rects }) {
            const style = floating.style;
            style.setProperty("--available-width", `${availableWidth}px`);
            style.setProperty("--available-height", `${availableHeight}px`);
            // Snap the anchor size to device pixels, so the popup's visual
            // width matches the anchor's.
            const dpr = floating.ownerDocument.defaultView.devicePixelRatio || 1;
            const { x, y, width, height } = rects.reference;
            const anchorWidth = (Math.round((x + width) * dpr) - Math.round(x * dpr)) / dpr;
            const anchorHeight = (Math.round((y + height) * dpr) - Math.round(y * dpr)) / dpr;
            style.setProperty("--anchor-width", `${anchorWidth}px`);
            style.setProperty("--anchor-height", `${anchorHeight}px`);
          },
        }),
        // transform-origin relies on an arrow element, a detached one when
        // the popup has none.
        arrowMiddleware({ element: arrowEl || document.createElement("div"), padding: arrowPadding }),
        {
          name: "transformOrigin",
          fn(state) {
            const { elements, middlewareData, placement: renderedPlacement, rects, y } = state;
            const currentRenderedSide = getSide(renderedPlacement);
            const currentRenderedAxis = getSideAxis(currentRenderedSide);
            const arrowX = middlewareData.arrow?.x || 0;
            const arrowY = middlewareData.arrow?.y || 0;
            const arrowWidth = arrowEl?.clientWidth || 0;
            const arrowHeight = arrowEl?.clientHeight || 0;
            const transformX = arrowX + arrowWidth / 2;
            const transformY = arrowY + arrowHeight / 2;
            const shiftY = Math.abs(middlewareData.shift?.y || 0);
            const halfAnchorHeight = rects.reference.height / 2;
            const sideOffsetValue = resolveOffset(sideOffset, state);
            const isOverlappingAnchor = shiftY > sideOffsetValue;
            const adjacentTransformOrigin = {
              top: `${transformX}px calc(100% + ${sideOffsetValue}px)`,
              bottom: `${transformX}px ${-sideOffsetValue}px`,
              left: `calc(100% + ${sideOffsetValue}px) ${transformY}px`,
              right: `${-sideOffsetValue}px ${transformY}px`,
            }[currentRenderedSide];
            const overlapTransformOrigin = `${transformX}px ${rects.reference.y + halfAnchorHeight - y}px`;
            elements.floating.style.setProperty(
              "--transform-origin",
              crossAxisShiftEnabled && currentRenderedAxis === "y" && isOverlappingAnchor
                ? overlapTransformOrigin
                : adjacentTransformOrigin,
            );
            return {};
          },
        },
        hideMiddleware(),
      );
      return list.filter(Boolean);
    }

    function placement() {
      const side = mountSide || {
        top: "top",
        right: "right",
        bottom: "bottom",
        left: "left",
        "inline-end": isRtl ? "left" : "right",
        "inline-start": isRtl ? "right" : "left",
      }[sideParam];
      return align === "center" ? side : `${side}-${align}`;
    }

    // Not positioned yet: fixed, so autoFocus does not scroll, and invisible.
    positioner.style.position = "fixed";
    positioner.style.opacity = "0";

    let active = true;
    let resolvePositioned;
    const positioned = new Promise((resolve) => {
      resolvePositioned = resolve;
    });

    function update() {
      return computePosition(anchor, positioner, {
        placement: placement(),
        strategy: positionMethod,
        middleware: middleware(),
      }).then((result) => {
        if (!active) return;
        const style = positioner.style;
        const renderedSide = getSide(result.placement);
        const renderedAlign = getAlignment(result.placement) || "center";
        const logicalSide = getLogicalSide(sideParam, renderedSide, isRtl);
        style.opacity = "";
        if (applyPosition()) {
          // floatingStyles of useFloating with transform: true
          style.position = positionMethod;
          style.left = "0px";
          style.top = "0px";
          style.transform = `translate(${roundByDPR(positioner, result.x)}px, ${roundByDPR(positioner, result.y)}px)`;
          if ((positioner.ownerDocument.defaultView.devicePixelRatio || 1) >= 1.5) style.willChange = "transform";
          parts.forEach((part) => part.setAttribute("data-side", logicalSide));
        }
        parts.forEach((part) => part.setAttribute("data-align", renderedAlign));
        positioner.toggleAttribute("data-anchor-hidden", !!result.middlewareData.hide?.referenceHidden);
        if (arrowEl) {
          arrowEl.style.position = "absolute";
          arrowEl.style.left = result.middlewareData.arrow?.x != null ? `${result.middlewareData.arrow.x}px` : "";
          arrowEl.style.top = result.middlewareData.arrow?.y != null ? `${result.middlewareData.arrow.y}px` : "";
          arrowEl.toggleAttribute("data-uncentered", result.middlewareData.arrow?.centerOffset !== 0);
        }
        // lazyFlip locks the side once positioned, so a filtered list that
        // resizes does not flip back and forth.
        if (lazyFlip) mountSide = renderedSide;
        onPosition?.(result, logicalSide);
        resolvePositioned();
      });
    }

    const stopAutoUpdate = autoUpdate(anchor, positioner, update, {
      elementResize: !disableAnchorTracking && typeof ResizeObserver !== "undefined",
      layoutShift: !disableAnchorTracking && typeof IntersectionObserver !== "undefined",
    });

    return {
      positioned,
      update,
      cleanup() {
        active = false;
        stopAutoUpdate();
        resolvePositioned();
      },
    };
  }

  window.templ = window.templ || {};
  window.templ.anchorPositioning = { useAnchorPositioning };
})();

// components/baseui/use_collapsible_panel.js
// Port of @base-ui/react collapsible/panel/useCollapsiblePanel.ts (1.6.0), the
// panel of Collapsible and Accordion. How a panel opens and closes depends on
// its motion, read from its computed style:
// - none: open and close at once, no starting or ending style.
// - CSS transition or keyframe animation: measured to pixels, starting style
//   for one frame on open, ending style one frame after close, hidden once the
//   animations finished, then back to auto.
// The size is exposed as <prefix>-height and <prefix>-width, for example
// --collapsible-panel-height or --accordion-panel-height.
//
// Left out: hidden="until-found" with find in page, which no shadcn panel
// uses, and React.Activity.
(function () {
  "use strict";

  const transition = () => window.templ.transition;

  function getAnimationType(panel, hasSuppressedMountAnimation) {
    const style = getComputedStyle(panel);
    const nonZero = (value) => value.split(",").some((part) => Number.parseFloat(part) > 0);
    const hasAnimation = (style.animationName.split(",").some((name) => name.trim() !== "none" && name.trim() !== "") ||
      hasSuppressedMountAnimation) && nonZero(style.animationDuration);
    const hasTransition = nonZero(style.transitionDuration);
    if (hasTransition) return "css-transition";
    if (hasAnimation) return "css-animation";
    return "none";
  }

  function setDimensions(panel, prefix, dimensions) {
    const px = (value) => (value === undefined ? "auto" : value + "px");
    panel.style.setProperty(prefix + "-height", px(dimensions?.height));
    panel.style.setProperty(prefix + "-width", px(dimensions?.width));
  }

  function getDimensions(panel) {
    return { height: panel.scrollHeight, width: panel.scrollWidth };
  }

  // Resets alignment styles that distort scroll sizes while measuring.
  function measureWithoutLayoutStyles(panel) {
    const keys = ["justify-content", "align-items", "align-content", "justify-items"];
    const original = keys.map((key) => panel.style.getPropertyValue(key));
    keys.forEach((key) => panel.style.setProperty(key, "initial", "important"));
    const dimensions = getDimensions(panel);
    requestAnimationFrame(() => keys.forEach((key, i) => {
      if (original[i] === "") panel.style.removeProperty(key);
      else panel.style.setProperty(key, original[i]);
    }));
    return dimensions;
  }

  // A panel that renders open skips its keyframe mount animation until it has
  // been closed once, so the server rendered first paint does not shift.
  function mount(panel, prefix, isOpen) {
    setDimensions(panel, prefix, null);
    if (!isOpen) return;
    panel._templPreventMountAnimation = true;
    panel.style.setProperty("animation-name", "none");
  }

  // The type is read after the new state applied, since the motion classes
  // hang on data-open and data-closed, like the source's layout effect after
  // the render with the new state.
  function open(panel, prefix) {
    panel.hidden = false;
    transition().open([panel], panel, () => setDimensions(panel, prefix, null));
    const type = getAnimationType(panel, false);
    if (type === "none") {
      transition().reset([panel], true);
      setDimensions(panel, prefix, null);
      return;
    }
    setDimensions(panel, prefix, type === "css-transition" ? measureWithoutLayoutStyles(panel) : getDimensions(panel));
  }

  function close(panel, prefix) {
    const hasSuppressedMountAnimation = !!panel._templPreventMountAnimation;
    if (hasSuppressedMountAnimation) {
      panel._templPreventMountAnimation = false;
      panel.style.removeProperty("animation-name");
    }
    const unmount = () => {
      panel.hidden = true;
      setDimensions(panel, prefix, null);
    };
    transition().close([panel], panel, unmount, {
      deferEnding: true,
      onEnding() {
        const dimensions = getDimensions(panel);
        if (!dimensions.height && !dimensions.width) return false;
        setDimensions(panel, prefix, dimensions);
      },
    });
    const type = getAnimationType(panel, hasSuppressedMountAnimation);
    if (type === "none") {
      transition().reset([panel], false);
      unmount();
      return;
    }
    // Measured as soon as the close is requested, before the ending style
    // applies, so an interrupted open closes from its current size.
    setDimensions(panel, prefix, getDimensions(panel));
  }

  window.templ = window.templ || {};
  window.templ.collapsiblePanel = { mount, open, close };
})();

// components/baseui/use_dismiss.js
// Port of @base-ui/react floating-ui-react/hooks/useDismiss.ts (1.6.0).
// Closes an open popup on Escape and on a press outside of it.
//
// useDismiss(options) runs while the popup is open, like the source's effect:
// call it on open and call the cleanup it returns on close.
//
//   floating           the popup element (required)
//   reference          the trigger element, or a list of them (the input for a combobox)
//   onOpenChange(open, reason, event)  requests the close, returns true when accepted
//   escapeKey          true, or a function read on every key press
//   outsidePress       true, false, or a function(event) that returns whether to dismiss
//   outsidePressEvent  "sloppy" (press), "intentional" (click), { mouse, touch }, or a function
//   referencePress     false, true, or a function: a press on the reference closes
//   bubbles            true, or { escapeKey, outsidePress }
//
// What differs from the source comes from having no React tree:
// - Base UI marks events inside its React tree, portals included. Here that tree
//   is the DOM plus the portal owners from portal.js, see withinTree.
// - The floating tree is every open useDismiss: a child is one whose popup lies
//   inside this popup's tree.
// - Options React re-reads on every render may be functions, read per event.
(function () {
  "use strict";

  const webkit = typeof CSS !== "undefined" && !!CSS.supports?.("-webkit-backdrop-filter:none");

  // Open instances, the FloatingTree pendant.
  const instances = new Set();

  // IME composition: Escape that settles a composition closes the compose
  // menu, not the popup. Safari fires compositionend before keydown.
  let isComposing = false;
  let compositionTimer = 0;
  document.addEventListener("compositionstart", () => {
    clearTimeout(compositionTimer);
    isComposing = true;
  });
  document.addEventListener("compositionend", () => {
    compositionTimer = setTimeout(() => {
      isComposing = false;
    }, webkit ? 5 : 0);
  });

  function getTarget(event) {
    return event.composedPath?.()[0] || event.target;
  }

  function contains(parent, child) {
    return !!parent && !!child && (parent === child || parent.contains(child));
  }

  // The React tree pendant: up through the DOM, and from a portaled element
  // on to the place it was declared.
  function withinTree(root, target) {
    for (let node = target; node; node = window.templ.portal.treeParent(node)) {
      if (node === root) return true;
    }
    return false;
  }

  function isLastTraversableNode(node) {
    return ["html", "body", "#document"].includes(node.nodeName.toLowerCase());
  }

  function normalizeBubbles(bubbles) {
    return {
      escapeKey: typeof bubbles === "boolean" ? bubbles : bubbles?.escapeKey ?? false,
      outsidePress: typeof bubbles === "boolean" ? bubbles : bubbles?.outsidePress ?? true,
    };
  }

  function useDismiss(options) {
    const {
      floating,
      onOpenChange,
      escapeKey = true,
      outsidePress = true,
      outsidePressEvent = "sloppy",
      referencePress = false,
    } = options;
    const { reference } = options;
    const references = (reference instanceof Element ? [reference] : [...(reference || [])]).filter(Boolean);
    const bubbles = normalizeBubbles(options.bubbles);
    const self = { floating, bubbles, active: true };

    let pressStartedInside = false;
    let pressStartPrevented = false;
    // Ignore only the very next outside click after dragging from inside to outside.
    let suppressNextOutsideClick = false;
    let currentPointerType = "";
    let touchState = null;
    let cancelDismissOnEndTimer = 0;
    let preventedPressSuppressionTimer = 0;

    const read = (value) => (typeof value === "function" ? value() : value);

    function hasBlockingChild(bubbleKey) {
      for (const other of instances) {
        if (other !== self && withinTree(floating, other.floating) && !other.bubbles[bubbleKey]) return true;
      }
      return false;
    }

    function isEventWithinOwnElements(event) {
      const target = getTarget(event);
      return contains(floating, target) || references.some((reference) => contains(reference, target));
    }

    function close(reason, event) {
      return self.active && onOpenChange(false, reason, event);
    }

    function closeOnEscapeKeyDown(event) {
      if (!self.active || !read(escapeKey) || event.key !== "Escape") return;
      if (isComposing) return;
      if (!bubbles.escapeKey && hasBlockingChild("escapeKey")) return;
      if (close("escape-key", event)) event.preventDefault();
      if (!bubbles.escapeKey) event.stopPropagation();
    }

    // The popup's own press handlers (the source's floating props).
    function markPressStartedInside(event) {
      if (event.button !== 0) return;
      // Only presses that start in the popup's DOM subtree count as inside,
      // so a nested portal does not suppress the parent's dismissal.
      if (!contains(floating, getTarget(event))) return;
      if (!pressStartedInside) {
        pressStartedInside = true;
        pressStartPrevented = false;
      }
    }

    function markInsidePressStartPrevented(event) {
      if (event.defaultPrevented && pressStartedInside) pressStartPrevented = true;
    }

    function getOutsidePressEvent() {
      const type = currentPointerType === "pen" || !currentPointerType ? "mouse" : currentPointerType;
      const value = read(outsidePressEvent);
      return typeof value === "string" ? value : value[type];
    }

    function shouldIgnoreEvent(event) {
      const mode = getOutsidePressEvent();
      return (mode === "intentional" && event.type !== "click") || (mode === "sloppy" && event.type === "click");
    }

    function suppressImmediateOutsideClickAfterPreventedStart() {
      suppressNextOutsideClick = true;
      clearTimeout(preventedPressSuppressionTimer);
      preventedPressSuppressionTimer = setTimeout(() => {
        suppressNextOutsideClick = false;
      }, 0);
    }

    function resetPressStartState() {
      pressStartedInside = false;
      pressStartPrevented = false;
    }

    function closeOnPressOutside(event) {
      if (!self.active) return;
      if (shouldIgnoreEvent(event)) {
        // A new press began outside the popup and its trigger. Clear any
        // leftover drag-out suppression so this press's click can dismiss.
        if (event.type !== "click" && !isEventWithinOwnElements(event)) {
          clearTimeout(preventedPressSuppressionTimer);
          suppressNextOutsideClick = false;
        }
        return;
      }
      const target = getTarget(event);
      // Inside the popup's tree, portaled children included.
      if (withinTree(floating, target)) return;
      // Another trigger of this popup was pressed.
      if (references.some((reference) => contains(reference, target))) return;

      // A press on a third party element injected after the popup opened:
      // its top level ancestor carries none of the data-base-ui-inert markers
      // markOthers set when it opened.
      const targetRoot = target instanceof Element ? target.getRootNode() : null;
      const isShadowRoot = typeof ShadowRoot !== "undefined" && targetRoot instanceof ShadowRoot;
      const markers = Array.from((isShadowRoot ? targetRoot : floating.ownerDocument).querySelectorAll("[data-base-ui-inert]"));
      let targetRootAncestor = target instanceof Element ? target : null;
      while (targetRootAncestor && !isLastTraversableNode(targetRootAncestor)) {
        const nextParent = targetRootAncestor.assignedSlot || targetRootAncestor.parentNode ||
          (targetRootAncestor.parentNode instanceof ShadowRoot ? targetRootAncestor.parentNode.host : null);
        if (!nextParent || isLastTraversableNode(nextParent) || !(nextParent instanceof Element)) break;
        targetRootAncestor = nextParent;
      }
      if (markers.length && target instanceof Element && !target.matches("html,body") &&
        !contains(target, floating) && markers.every((marker) => !contains(targetRootAncestor, marker))) {
        return;
      }

      // A press on a scrollbar. Touch never hits a scrollbar.
      if (target instanceof HTMLElement && !("touches" in event)) {
        const lastTraversableNode = isLastTraversableNode(target);
        const style = getComputedStyle(target);
        const scrollRe = /auto|scroll/;
        const isScrollableX = lastTraversableNode || scrollRe.test(style.overflowX);
        const isScrollableY = lastTraversableNode || scrollRe.test(style.overflowY);
        const canScrollX = isScrollableX && target.clientWidth > 0 && target.scrollWidth > target.clientWidth;
        const canScrollY = isScrollableY && target.clientHeight > 0 && target.scrollHeight > target.clientHeight;
        const isRTL = style.direction === "rtl";
        const pressedVerticalScrollbar = canScrollY &&
          (isRTL ? event.offsetX <= target.offsetWidth - target.clientWidth : event.offsetX > target.clientWidth);
        const pressedHorizontalScrollbar = canScrollX && event.offsetY > target.clientHeight;
        if (pressedVerticalScrollbar || pressedHorizontalScrollbar) return;
      }

      // In intentional mode, a press that starts inside and ends outside gets
      // one suppressed outside click.
      if (getOutsidePressEvent() === "intentional" && suppressNextOutsideClick) {
        clearTimeout(preventedPressSuppressionTimer);
        suppressNextOutsideClick = false;
        return;
      }
      if (typeof outsidePress === "function" && !outsidePress(event)) return;
      if (hasBlockingChild("outsidePress")) return;
      close("outside-press", event);
    }

    function handlePointerDown(event) {
      if (getOutsidePressEvent() !== "sloppy" || event.pointerType === "touch" || isEventWithinOwnElements(event)) return;
      closeOnPressOutside(event);
    }

    function handleTouchStart(event) {
      if (getOutsidePressEvent() !== "sloppy" || isEventWithinOwnElements(event)) return;
      const touch = event.touches[0];
      if (!touch) return;
      touchState = {
        startX: touch.clientX,
        startY: touch.clientY,
        dismissOnTouchEnd: false,
        dismissOnMouseDown: true,
      };
      clearTimeout(cancelDismissOnEndTimer);
      cancelDismissOnEndTimer = setTimeout(() => {
        if (touchState) {
          touchState.dismissOnTouchEnd = false;
          touchState.dismissOnMouseDown = false;
        }
      }, 1000);
    }

    // Runs the listener after the target's own handlers, in the bubble phase
    // of the target, like the source.
    function addTargetEventListenerOnce(event, listener) {
      const target = getTarget(event);
      if (!target) return;
      const once = () => {
        target.removeEventListener(event.type, once);
        listener(event);
      };
      target.addEventListener(event.type, once);
    }

    function handleTouchStartCapture(event) {
      currentPointerType = "touch";
      addTargetEventListenerOnce(event, handleTouchStart);
    }

    function closeOnPressOutsideCapture(event) {
      clearTimeout(cancelDismissOnEndTimer);
      if (event.type === "pointerdown") currentPointerType = event.pointerType;
      if (event.type === "mousedown" && touchState && !touchState.dismissOnMouseDown) return;
      addTargetEventListenerOnce(event, (targetEvent) => {
        if (targetEvent.type === "pointerdown") handlePointerDown(targetEvent);
        else closeOnPressOutside(targetEvent);
      });
    }

    function handlePressEndCapture(event) {
      if (!pressStartedInside) return;
      const startPrevented = pressStartPrevented;
      resetPressStartState();
      if (getOutsidePressEvent() !== "intentional") return;
      if (event.type === "pointercancel") {
        if (startPrevented) suppressImmediateOutsideClickAfterPreventedStart();
        return;
      }
      if (withinTree(floating, getTarget(event))) return;
      if (startPrevented) {
        suppressImmediateOutsideClickAfterPreventedStart();
        return;
      }
      if (typeof outsidePress === "function" && !outsidePress(event)) return;
      clearTimeout(preventedPressSuppressionTimer);
      suppressNextOutsideClick = true;
    }

    function handleTouchMove(event) {
      if (getOutsidePressEvent() !== "sloppy" || !touchState || isEventWithinOwnElements(event)) return;
      const touch = event.touches[0];
      if (!touch) return;
      const distance = Math.hypot(touch.clientX - touchState.startX, touch.clientY - touchState.startY);
      if (distance > 5) touchState.dismissOnTouchEnd = true;
      if (distance > 10) {
        closeOnPressOutside(event);
        clearTimeout(cancelDismissOnEndTimer);
        touchState = null;
      }
    }

    function handleTouchEnd(event) {
      if (getOutsidePressEvent() !== "sloppy" || !touchState || isEventWithinOwnElements(event)) return;
      if (touchState.dismissOnTouchEnd) closeOnPressOutside(event);
      clearTimeout(cancelDismissOnEndTimer);
      touchState = null;
    }

    // The reference's onPointerDown and onClick.
    function closeOnReferencePress(event) {
      if (!read(referencePress)) return;
      close("trigger-press", event);
    }

    const listeners = [
      ...references.map((reference) => [reference, "pointerdown", closeOnReferencePress]),
      ...references.map((reference) => [reference, "click", closeOnReferencePress]),
      [document, "keydown", closeOnEscapeKeyDown],
      ...references.map((reference) => [reference, "keydown", closeOnEscapeKeyDown]),
      [floating, "keydown", closeOnEscapeKeyDown],
      [floating, "pointerdown", markPressStartedInside, true],
      [floating, "mousedown", markPressStartedInside, true],
      [floating, "pointerdown", markInsidePressStartPrevented],
      [floating, "mousedown", markInsidePressStartPrevented],
    ];
    if (outsidePress !== false) {
      listeners.push(
        [document, "click", closeOnPressOutsideCapture, true],
        [document, "pointerdown", closeOnPressOutsideCapture, true],
        [document, "pointerup", handlePressEndCapture, true],
        [document, "pointercancel", handlePressEndCapture, true],
        [document, "mousedown", closeOnPressOutsideCapture, true],
        [document, "mouseup", handlePressEndCapture, true],
        [document, "touchstart", handleTouchStartCapture, true],
        [document, "touchmove", (event) => addTargetEventListenerOnce(event, handleTouchMove), true],
        [document, "touchend", (event) => addTargetEventListenerOnce(event, handleTouchEnd), true],
      );
    }
    listeners.forEach(([target, type, listener, capture]) => target.addEventListener(type, listener, !!capture));
    instances.add(self);

    return function cleanup() {
      if (!self.active) return;
      self.active = false;
      instances.delete(self);
      listeners.forEach(([target, type, listener, capture]) => target.removeEventListener(type, listener, !!capture));
      clearTimeout(cancelDismissOnEndTimer);
      clearTimeout(preventedPressSuppressionTimer);
    };
  }

  window.templ = window.templ || {};
  window.templ.dismiss = { useDismiss };
})();

// components/baseui/use_list_navigation.js
// Port of @base-ui/react floating-ui-react/hooks/useListNavigation.ts (1.6.0)
// with the list helpers of floating-ui-react/utils/composite.ts. Arrow key
// navigation of a list, with real focus or, for a combobox, virtual focus.
//
//   const nav = window.templ.listNavigation.useListNavigation(options)   when the component mounts
//   nav.open()     after the popup opened, the source's open effects
//   nav.close()    when it closes
//   nav.sync()     after the component changed its active index itself
//   nav.cleanup()  when the component unmounts
//
//   floating            the popup, the element that takes focus
//   reference           the trigger, or the input of a combobox
//   items()             the list, in order (listRef)
//   activeIndex()       the component's active index, or null
//   selectedIndex()     the selected index, or null
//   onNavigate(index, event)   sets the component's active index (null for none)
//   onOpenChange(open, reason, event)  opens on an arrow key, closes a nested list
//   isOpen()
//   loopFocus, nested, rtl, virtual, orientation, parentOrientation, allowEscape,
//   focusItemOnOpen ("auto", true, false), focusItemOnHover, openOnArrowKeyDown,
//   resetOnPointerLeave, disabledIndices (an array, a function, or undefined)
//   onVirtualFocus(item)  for virtual lists, the source's virtualfocus event
//
// Left out: grid navigation, which only a grid combobox uses.
(function () {
  "use strict";

  const t = () => window.templ.tabbable;
  const ARROW_UP = "ArrowUp";
  const ARROW_DOWN = "ArrowDown";
  const ARROW_LEFT = "ArrowLeft";
  const ARROW_RIGHT = "ArrowRight";

  function doSwitch(orientation, vertical, horizontal) {
    switch (orientation) {
      case "vertical": return vertical;
      case "horizontal": return horizontal;
      default: return vertical || horizontal;
    }
  }

  function isMainOrientationKey(key, orientation) {
    const vertical = key === ARROW_UP || key === ARROW_DOWN;
    const horizontal = key === ARROW_LEFT || key === ARROW_RIGHT;
    return doSwitch(orientation, vertical, horizontal);
  }

  function isMainOrientationToEndKey(key, orientation, rtl) {
    const vertical = key === ARROW_DOWN;
    const horizontal = rtl ? key === ARROW_LEFT : key === ARROW_RIGHT;
    return doSwitch(orientation, vertical, horizontal) || key === "Enter" || key === " " || key === "";
  }

  function isCrossOrientationOpenKey(key, orientation, rtl) {
    const vertical = rtl ? key === ARROW_LEFT : key === ARROW_RIGHT;
    const horizontal = key === ARROW_DOWN;
    return doSwitch(orientation, vertical, horizontal);
  }

  function isCrossOrientationCloseKey(key, orientation, rtl) {
    const vertical = rtl ? key === ARROW_RIGHT : key === ARROW_LEFT;
    const horizontal = key === ARROW_UP;
    if (orientation === "both") return key === "Escape";
    return doSwitch(orientation, vertical, horizontal);
  }

  // utils/composite.ts, see composite.js.
  const c = () => window.templ.composite;
  const isIndexOutOfListBounds = (...args) => c().isIndexOutOfListBounds(...args);
  const isListIndexDisabled = (...args) => c().isListIndexDisabled(...args);
  const findNonDisabledListIndex = (...args) => c().findNonDisabledListIndex(...args);
  const getMinListIndex = (...args) => c().getMinListIndex(...args);
  const getMaxListIndex = (...args) => c().getMaxListIndex(...args);

  const isTypeableCombobox = (element) => !!element && element.getAttribute("role") === "combobox" &&
    element.matches("input:not([type='hidden']):not([disabled]),[contenteditable]:not([contenteditable='false']),textarea:not([disabled])");
  const stopEvent = (event) => {
    event.preventDefault();
    event.stopPropagation();
  };

  function isVirtualClick(event) {
    if (event.pointerType === "" && event.isTrusted) return true;
    return event.detail === 0 && !event.pointerType;
  }

  function isVirtualPointerEvent(event) {
    return (event.width === 0 && event.height === 0) ||
      (event.width < 1 && event.height < 1 && event.pressure === 0 && event.detail === 0 && event.pointerType === "touch");
  }

  // ----- useListNavigation -------------------------------------------------------

  function useListNavigation(options) {
    const {
      floating,
      reference = null,
      items,
      activeIndex = () => null,
      selectedIndex = () => null,
      onNavigate: onNavigateProp = () => {},
      onOpenChange = () => {},
      isOpen,
      allowEscape = false,
      loopFocus = false,
      nested = false,
      rtl = false,
      virtual = false,
      focusItemOnOpen = "auto",
      focusItemOnHover = true,
      openOnArrowKeyDown = true,
      disabledIndices,
      orientation = "vertical",
      parentOrientation,
      resetOnPointerLeave = true,
      onVirtualFocus,
    } = options;
    const typeableComboboxReference = isTypeableCombobox(reference);
    let focusItemOnOpenState = focusItemOnOpen;
    let indexRef = selectedIndex() ?? -1;
    let key = null;
    let isPointerModality = true;
    let forceSyncFocus = false;
    let forceScrollIntoView = false;
    let focusFrame = 0;
    let cancelQueuedFocus = null;
    const cleanups = [];

    const list = () => items();
    const onNavigate = (event) => {
      onNavigateProp(indexRef === -1 ? null : indexRef, event);
      sync();
    };

    function focusItem() {
      const runFocus = (item) => {
        if (virtual) onVirtualFocus?.(item);
        else cancelQueuedFocus = window.templ.focusManager.enqueueFocus(item, { sync: forceSyncFocus, preventScroll: true });
      };
      const initialItem = list()[indexRef];
      const scrollIntoView = forceScrollIntoView;
      if (initialItem) runFocus(initialItem);
      const run = () => {
        const waitedItem = list()[indexRef] || initialItem;
        if (!waitedItem) return;
        if (!initialItem) runFocus(waitedItem);
        if (scrollIntoView || !isPointerModality) waitedItem.scrollIntoView?.({ block: "nearest", inline: "nearest" });
      };
      if (forceSyncFocus) {
        run();
      } else {
        cancelAnimationFrame(focusFrame);
        focusFrame = requestAnimationFrame(run);
      }
    }

    // The source's effect on activeIndex: focus the active item while open.
    function sync() {
      if (!isOpen()) {
        forceSyncFocus = false;
        return;
      }
      const active = activeIndex();
      if (active == null) {
        forceSyncFocus = false;
        return;
      }
      if (!isIndexOutOfListBounds(list(), active)) {
        indexRef = active;
        focusItem();
        forceScrollIntoView = false;
      }
    }

    // The source's open effects: start at the selected item, or focus the
    // first or last item when a key or a virtual click opened the list.
    function open() {
      indexRef = selectedIndex() ?? -1;
      if (focusItemOnOpenState && selectedIndex() != null) {
        // The selected item comes into view regardless of the modality.
        forceScrollIntoView = true;
        onNavigate();
        return;
      }
      if (activeIndex() != null) {
        sync();
        return;
      }
      if (focusItemOnOpenState && (key != null || (focusItemOnOpenState === true && key == null))) {
        let runs = 0;
        const waitForListPopulated = () => {
          if (list()[0] == null) {
            if (runs < 2) (runs ? requestAnimationFrame : queueMicrotask)(waitForListPopulated);
            runs += 1;
            return;
          }
          // The first non disabled item, skipping attribute disabled ones
          // even with an empty disabledIndices (mui/base-ui#2604).
          indexRef = key == null || isMainOrientationToEndKey(key, orientation, rtl) || nested
            ? getMinListIndex(list())
            : getMaxListIndex(list());
          key = null;
          onNavigate();
        };
        waitForListPopulated();
      }
    }

    function close() {
      indexRef = -1;
      onNavigateProp(null);
      forceSyncFocus = false;
      key = null;
      focusItemOnOpenState = focusItemOnOpen;
    }

    const on = (target, type, listener, capture) => {
      if (!target) return;
      target.addEventListener(type, listener, !!capture);
      cleanups.push(() => target.removeEventListener(type, listener, !!capture));
    };

    function syncCurrentTarget(event, item) {
      if (!isOpen()) return;
      const index = list().indexOf(item);
      if (index !== -1 && (indexRef !== index || activeIndex() !== index)) {
        indexRef = index;
        onNavigate(event);
      }
    }

    function commonOnKeyDown(event) {
      isPointerModality = false;
      forceSyncFocus = true;
      // Chrome fires ArrowDown twice while composing.
      if (event.which === 229) return;
      // Animating out: ignore navigation.
      if (!isOpen() && event.currentTarget === floating) return;

      if (nested && isCrossOrientationCloseKey(event.key, orientation, rtl)) {
        // The parent navigates with this key too: let it.
        if (!isMainOrientationKey(event.key, parentOrientation)) stopEvent(event);
        onOpenChange(false, "list-navigation", event);
        if (reference instanceof HTMLElement) {
          if (virtual) onVirtualFocus?.(reference);
          else reference.focus();
        }
        return;
      }
      const currentIndex = indexRef;
      const items = list();
      const minIndex = getMinListIndex(items, disabledIndices);
      const maxIndex = getMaxListIndex(items, disabledIndices);
      if (!typeableComboboxReference) {
        if (event.key === "Home") {
          stopEvent(event);
          indexRef = minIndex;
          onNavigate(event);
        }
        if (event.key === "End") {
          stopEvent(event);
          indexRef = maxIndex;
          onNavigate(event);
        }
      }
      if (!isMainOrientationKey(event.key, orientation)) return;
      stopEvent(event);
      // No item focused: start at the end the key points to.
      if (isOpen() && !virtual && t().activeElement(event.currentTarget.ownerDocument) === event.currentTarget) {
        indexRef = isMainOrientationToEndKey(event.key, orientation, rtl) ? minIndex : maxIndex;
        onNavigate(event);
        return;
      }
      if (isMainOrientationToEndKey(event.key, orientation, rtl)) {
        if (loopFocus) {
          if (currentIndex >= maxIndex) {
            if (allowEscape && currentIndex !== items.length) {
              indexRef = -1;
            } else {
              forceSyncFocus = false;
              indexRef = minIndex;
            }
          } else {
            indexRef = findNonDisabledListIndex(items, { startingIndex: currentIndex, disabledIndices });
          }
        } else {
          indexRef = Math.min(maxIndex, findNonDisabledListIndex(items, { startingIndex: currentIndex, disabledIndices }));
        }
      } else if (loopFocus) {
        if (currentIndex <= minIndex) {
          if (allowEscape && currentIndex !== -1) {
            indexRef = items.length;
          } else {
            forceSyncFocus = false;
            indexRef = maxIndex;
          }
        } else {
          indexRef = findNonDisabledListIndex(items, { startingIndex: currentIndex, decrement: true, disabledIndices });
        }
      } else {
        indexRef = Math.max(minIndex, findNonDisabledListIndex(items, { startingIndex: currentIndex, decrement: true, disabledIndices }));
      }
      if (isIndexOutOfListBounds(items, indexRef)) indexRef = -1;
      onNavigate(event);
    }

    // The floating element's props.
    on(floating, "keydown", (event) => {
      // Shift+Tab closes a submenu. An element nested in the popup with its
      // own focus management, like a dialog opened from the menu, keeps it.
      if (event.key === "Tab" && event.shiftKey && isOpen() && !virtual) {
        if (!t().contains(floating, event.composedPath?.()[0] || event.target)) return;
        stopEvent(event);
        onOpenChange(false, "focus-out", event);
        if (reference instanceof HTMLElement) reference.focus();
        return;
      }
      commonOnKeyDown(event);
    });
    on(floating, "pointermove", () => {
      isPointerModality = true;
    });
    if (!virtual && !typeableComboboxReference) floating?.setAttribute("aria-orientation", orientation);

    // The items' props, delegated since the list changes.
    const itemOf = (target) => {
      const items = list();
      for (let node = target; node && node !== floating; node = node.parentElement) {
        if (items.includes(node)) return node;
      }
      return null;
    };
    on(floating, "focusin", (event) => {
      const item = itemOf(event.target);
      if (!item) return;
      forceSyncFocus = true;
      syncCurrentTarget(event, item);
    });
    on(floating, "click", (event) => {
      itemOf(event.target)?.focus({ preventScroll: true });
    });
    on(floating, "mousemove", (event) => {
      const item = itemOf(event.target);
      if (!item) return;
      forceSyncFocus = true;
      forceScrollIntoView = false;
      if (focusItemOnHover) syncCurrentTarget(event, item);
    });
    on(floating, "pointerout", (event) => {
      const item = itemOf(event.target);
      if (!item || item.contains(event.relatedTarget)) return;
      if (!isOpen() || !isPointerModality || event.pointerType === "touch") return;
      forceSyncFocus = true;
      if (!focusItemOnHover || list().includes(event.relatedTarget) || !resetOnPointerLeave) return;
      cancelQueuedFocus?.();
      cancelQueuedFocus = null;
      indexRef = -1;
      onNavigate(event);
      if (!virtual && t().contains(floating, t().activeElement(floating.ownerDocument))) {
        floating.focus({ preventScroll: true });
      }
    });

    // The trigger's props.
    function openOnNavigationKeyDown(event) {
      onOpenChange(true, "list-navigation", event);
    }
    on(reference, "keydown", (event) => {
      const currentOpen = isOpen();
      isPointerModality = false;
      const isArrowKey = event.key.startsWith("Arrow");
      const isParentCrossOpenKey = isCrossOrientationOpenKey(event.key, parentOrientation, rtl);
      const isMainKey = isMainOrientationKey(event.key, orientation);
      const isNavigationKey = (nested ? isParentCrossOpenKey : isMainKey) || event.key === "Enter" || event.key.trim() === "";
      if (virtual && currentOpen) {
        commonOnKeyDown(event);
        return;
      }
      if (!currentOpen && !openOnArrowKeyDown && isArrowKey) return;
      if (isNavigationKey) {
        const isParentMainKey = isMainOrientationKey(event.key, parentOrientation);
        key = nested && isParentMainKey ? null : event.key;
      }
      if (nested) {
        if (isParentCrossOpenKey) {
          stopEvent(event);
          if (currentOpen) {
            indexRef = getMinListIndex(list(), disabledIndices);
            onNavigate(event);
          } else {
            openOnNavigationKeyDown(event);
          }
        }
        return;
      }
      if (isMainKey) {
        if (selectedIndex() != null) indexRef = selectedIndex();
        stopEvent(event);
        if (!currentOpen && openOnArrowKeyDown) openOnNavigationKeyDown(event);
        else commonOnKeyDown(event);
        if (currentOpen) onNavigate(event);
      }
    });
    on(reference, "focus", (event) => {
      if (isOpen() && !virtual) {
        indexRef = -1;
        onNavigate(event);
      }
    });
    const checkVirtualPointer = (event) => {
      // pointerdown fires first: reset, then check.
      focusItemOnOpenState = focusItemOnOpen;
      if (focusItemOnOpen === "auto" && isVirtualPointerEvent(event)) focusItemOnOpenState = true;
    };
    const checkVirtualMouse = (event) => {
      if (focusItemOnOpen === "auto" && isVirtualClick(event)) focusItemOnOpenState = !virtual;
    };
    on(reference, "pointerdown", checkVirtualPointer);
    on(reference, "pointerenter", checkVirtualPointer);
    on(reference, "mousedown", checkVirtualMouse);
    on(reference, "click", checkVirtualMouse);

    return {
      open,
      close,
      sync,
      cleanup() {
        cancelAnimationFrame(focusFrame);
        cleanups.splice(0).forEach((cleanup) => cleanup());
      },
    };
  }

  window.templ = window.templ || {};
  window.templ.listNavigation = { useListNavigation };
})();

// components/baseui/use_transition_status.js
// Port of @base-ui/react internals/useTransitionStatus.ts with
// useOpenChangeComplete.ts and useAnimationsFinished.ts (1.6.0).
//
// The popup and its backdrop render the status: data-open or data-closed,
// plus data-starting-style for the first frame of an open and
// data-ending-style while it closes. The positioner and the arrow render only
// data-open or data-closed (popupStateMapping), and the positioner has its
// transitions off while starting (getDisabledMountTransitionStyles) and no
// pointer events while closed (usePositioner's inert). The close
// completes once every animation on the animated element finished, which is
// when Base UI unmounts. Opening again before that cancels the pending close.
//
// status is an array of the parts that render the status, or
// { parts, positioner, stateParts }. parts[0] carries the pending status.
(function () {
  "use strict";

  function set(parts, name, present) {
    parts.forEach((part) => part?.toggleAttribute(name, present));
  }

  function resolve(status) {
    const { parts, positioner = null, stateParts = [] } = Array.isArray(status) ? { parts: status } : status;
    return { parts: parts.filter(Boolean), open: [positioner, ...stateParts].filter(Boolean), positioner };
  }

  // usePositioner's inert: a closed positioner takes no pointer events, also
  // during its exit animation. Opening only takes back that none, so a value
  // safePolygon set stays, as React leaves a style it did not render.
  function setOpen(status, isOpen) {
    const all = [...status.parts, ...status.open];
    set(all, "data-open", isOpen);
    set(all, "data-closed", !isOpen);
    const style = status.positioner?.style;
    if (!style) return;
    if (!isOpen) style.pointerEvents = "none";
    else if (style.pointerEvents === "none") style.pointerEvents = "";
  }

  // useAnimationsFinished: runs fn once every animation on the element
  // finished, as long as isCurrent() still holds. An animation aborted
  // because a property it depends on changed may be followed by a new one,
  // so check again before running fn.
  function whenAnimationsFinish(animated, isCurrent, fn) {
    const exec = () => {
      if (!isCurrent()) return;
      if (typeof animated?.getAnimations !== "function") return fn();
      Promise.all(animated.getAnimations().map((animation) => animation.finished)).then(() => {
        if (isCurrent()) fn();
      }, () => {
        const running = animated.getAnimations().some((a) => a.pending || a.playState !== "finished");
        if (running) exec();
        else if (isCurrent()) fn();
      });
    };
    // One frame, so the new style's animations are registered.
    requestAnimationFrame(exec);
  }

  // onComplete, when given, runs once the open animations on animated
  // finished (useOpenChangeComplete with open true).
  function open(statusParam, animated, onComplete) {
    const status = resolve(statusParam);
    const { parts, positioner } = status;
    const main = parts[0];
    const token = {};
    main._templTransition = token;
    set(parts, "data-ending-style", false);
    setOpen(status, true);
    set(parts, "data-starting-style", true);
    if (positioner) positioner.style.transition = "none";
    // Compute the starting style once, so transitions start from it.
    void main.offsetWidth;
    requestAnimationFrame(() => {
      if (main._templTransition !== token) return;
      set(parts, "data-starting-style", false);
      if (positioner) positioner.style.transition = "";
      if (onComplete) whenAnimationsFinish(animated, () => main._templTransition === token, onComplete);
    });
  }

  // deferEnding sets data-ending-style one frame after data-closed, Base UI's
  // deferEndingState, and calls onEnding then. onEnding returning false
  // completes the close at once.
  function close(statusParam, animated, onComplete, { deferEnding = false, onEnding } = {}) {
    const status = resolve(statusParam);
    const { parts, positioner } = status;
    const main = parts[0];
    const token = {};
    main._templTransition = token;
    set(parts, "data-starting-style", false);
    if (positioner) positioner.style.transition = "";
    setOpen(status, false);

    const done = () => {
      if (main._templTransition !== token) return;
      main._templTransition = null;
      set(parts, "data-ending-style", false);
      onComplete?.();
    };
    const ending = () => {
      set(parts, "data-ending-style", true);
      if (onEnding?.() === false) return done();
      whenAnimationsFinish(animated, () => main._templTransition === token, done);
    };
    if (!deferEnding) return ending();
    requestAnimationFrame(() => {
      if (main._templTransition === token) ending();
    });
  }

  // Sets the state without a transition, like an unmount without exit
  // animation, and cancels one in flight.
  function reset(statusParam, isOpen) {
    const status = resolve(statusParam);
    status.parts[0]._templTransition = null;
    set(status.parts, "data-starting-style", false);
    set(status.parts, "data-ending-style", false);
    if (status.positioner) status.positioner.style.transition = "";
    setOpen(status, isOpen);
  }

  // Whether a close is in flight, Base UI's transitionStatus === "ending".
  function isEnding(element) {
    return !!element?.hasAttribute("data-ending-style");
  }

  // useAnimationsFinished on its own, for parts whose status another store
  // drives, like the toast manager.
  function animationsFinished(element, fn) {
    whenAnimationsFinish(element, () => element.isConnected, fn);
  }

  window.templ = window.templ || {};
  window.templ.transition = { open, close, reset, isEnding, animationsFinished };
})();

// components/baseui/use_typeahead.js
// Port of @base-ui/react floating-ui-react/hooks/useTypeahead.ts (1.6.0).
// Typing a character moves to the next item whose label starts with what was
// typed, until the typing pauses for resetMs.
//
//   const typeahead = window.templ.typeahead.useTypeahead(options)
//   typeahead.reset()     when the popup opens or closes, the source's effect
//   typeahead.cleanup()
//
//   elements        the reference and the floating element, which listen
//   labels()        the items' labels, in list order (listRef)
//   items()         the items, for the visibility check (elementsRef)
//   activeIndex()   selectedIndex()   isOpen()
//   onMatch(index)  onTyping(typing)
//   disabledIndices an array, a function, or undefined
//   resetMs         default 750, Base UI's menus and selects pass 500
(function () {
  "use strict";

  const t = () => window.templ.tabbable;

  function useTypeahead(options) {
    const {
      elements,
      labels,
      items = () => [],
      activeIndex = () => null,
      selectedIndex = () => null,
      isOpen,
      onMatch,
      onTyping,
      disabledIndices,
      resetMs = 750,
    } = options;
    let string = "";
    let prevIndex = selectedIndex() ?? activeIndex() ?? -1;
    let matchIndex = null;
    let timer = 0;
    const cleanups = [];
    const stopEvent = (event) => {
      event.preventDefault();
      event.stopPropagation();
    };

    function isItemAvailable(index) {
      const element = items()[index];
      if (element && !t().isElementVisible(element)) return false;
      return disabledIndices == null || !window.templ.composite.isListIndexDisabled([], index, disabledIndices);
    }

    function getMatchingIndex(list, value, startIndex = 0) {
      if (list.length === 0) return -1;
      const normalizedStartIndex = ((startIndex % list.length) + list.length) % list.length;
      const lowerString = value.toLowerCase();
      for (let offset = 0; offset < list.length; offset += 1) {
        const index = (normalizedStartIndex + offset) % list.length;
        const text = list[index];
        if (!text?.toLowerCase().startsWith(lowerString) || !isItemAvailable(index)) continue;
        return index;
      }
      return -1;
    }

    function onKeyDown(event) {
      const listContent = labels();
      // Space continues a typeahead session in progress.
      if (string.length > 0 && event.key === " ") {
        stopEvent(event);
        onTyping?.(true);
      }
      if (string.length > 0 && string[0] !== " ") {
        if (getMatchingIndex(listContent, string) === -1 && event.key !== " ") onTyping?.(false);
      }
      if (listContent == null || event.key.length !== 1 || event.ctrlKey || event.metaKey || event.altKey) return;
      if (isOpen() && event.key !== " ") {
        stopEvent(event);
        onTyping?.(true);
      }
      const isNewSession = string === "";
      if (isNewSession) prevIndex = selectedIndex() ?? activeIndex() ?? -1;
      // Rapid presses of one letter cycle through the items starting with it,
      // unless a label repeats its first letter, like "llama".
      const allowRapidSuccessionOfFirstLetter = listContent.every((text, index) =>
        text && isItemAvailable(index) ? text[0]?.toLowerCase() !== text[1]?.toLowerCase() : true);
      if (allowRapidSuccessionOfFirstLetter && string === event.key) {
        string = "";
        prevIndex = matchIndex;
      }
      string += event.key;
      clearTimeout(timer);
      timer = setTimeout(() => {
        string = "";
        prevIndex = matchIndex;
        onTyping?.(false);
      }, resetMs);
      const start = (isNewSession ? selectedIndex() ?? activeIndex() ?? -1 : prevIndex) ?? 0;
      const index = getMatchingIndex(listContent, string, start + 1);
      if (index !== -1) {
        onMatch?.(index);
        matchIndex = index;
      } else if (event.key !== " ") {
        string = "";
        onTyping?.(false);
      }
    }

    // Leaving the reference and the popup ends the session.
    function onBlur(event) {
      const next = event.relatedTarget;
      if (elements.some((element) => t().contains(element, next))) return;
      clearTimeout(timer);
      string = "";
      prevIndex = matchIndex;
      onTyping?.(false);
    }

    elements.filter(Boolean).forEach((element) => {
      element.addEventListener("keydown", onKeyDown);
      element.addEventListener("focusout", onBlur);
      cleanups.push(() => {
        element.removeEventListener("keydown", onKeyDown);
        element.removeEventListener("focusout", onBlur);
      });
    });

    return {
      // The source's effects on open and selectedIndex.
      reset() {
        clearTimeout(timer);
        matchIndex = null;
        string = "";
        prevIndex = selectedIndex() ?? activeIndex() ?? -1;
      },
      cleanup() {
        clearTimeout(timer);
        cleanups.splice(0).forEach((cleanup) => cleanup());
      },
    };
  }

  window.templ = window.templ || {};
  window.templ.typeahead = { useTypeahead };
})();

// components/collapsible/collapsible.js
(function () {
  "use strict";

  const PANEL = '[data-slot="collapsible-content"]';

  function panelFor(trigger) {
    return document.getElementById(trigger.getAttribute("aria-controls") || "");
  }

  // A trigger merged onto another component keeps that component's slot
  // (Base UI render prop), so the trigger is whatever controls a panel.
  function triggerOf(target) {
    const trigger = target.closest("[aria-controls]");
    const panel = trigger && panelFor(trigger);
    return panel && panel.matches(PANEL) ? trigger : null;
  }

  function setOpen(el, isOpen) {
    el.toggleAttribute("data-open", isOpen);
    el.toggleAttribute("data-closed", !isOpen);
  }

  // Base UI's CollapsiblePanel exposes its size as these variables.
  const VARS = "--collapsible-panel";

  function toggle(trigger) {
    const panel = panelFor(trigger);
    if (!panel) return;
    const root = panel.closest('[data-slot="collapsible"]');
    if (!root || root.hasAttribute("data-disabled")) return;
    const isOpen = !panel.hasAttribute("data-open");
  const accepted = root.dispatchEvent(
    new CustomEvent("collapsible-open-change", {
      bubbles: true,
      cancelable: true,
      detail: { open: isOpen },
    }),
  );
  if (!accepted || root.hasAttribute("data-templ-open")) return;

    setOpen(root, isOpen);
    trigger.setAttribute("aria-expanded", isOpen ? "true" : "false");
    trigger.toggleAttribute("data-panel-open", isOpen);
    if (isOpen) window.templ.collapsiblePanel.open(panel, VARS);
    else window.templ.collapsiblePanel.close(panel, VARS);
  }

  document.addEventListener("click", (e) => {
    if (!(e.target instanceof Element)) return;
    const trigger = triggerOf(e.target);
    if (trigger) toggle(trigger);
  });

  window.templ.lifecycle.register(PANEL, {
    init: (panel) => window.templ.collapsiblePanel.mount(panel, VARS, !panel.hidden),
  });
})();

// components/floatingui/floating_ui_core.js
// @floating-ui/core 1.7.5, dist/floating-ui.core.umd.js, the version @base-ui/react
// 1.6.0 resolves at the pin in plans/UPSTREAM.md.
(function (global, factory) {
  typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports) :
  typeof define === 'function' && define.amd ? define(['exports'], factory) :
  (global = typeof globalThis !== 'undefined' ? globalThis : global || self, factory(global.FloatingUICore = {}));
})(this, (function (exports) { 'use strict';

  /**
   * Custom positioning reference element.
   * @see https://floating-ui.com/docs/virtual-elements
   */

  const sides = ['top', 'right', 'bottom', 'left'];
  const alignments = ['start', 'end'];
  const placements = /*#__PURE__*/sides.reduce((acc, side) => acc.concat(side, side + "-" + alignments[0], side + "-" + alignments[1]), []);
  const min = Math.min;
  const max = Math.max;
  const oppositeSideMap = {
    left: 'right',
    right: 'left',
    bottom: 'top',
    top: 'bottom'
  };
  function clamp(start, value, end) {
    return max(start, min(value, end));
  }
  function evaluate(value, param) {
    return typeof value === 'function' ? value(param) : value;
  }
  function getSide(placement) {
    return placement.split('-')[0];
  }
  function getAlignment(placement) {
    return placement.split('-')[1];
  }
  function getOppositeAxis(axis) {
    return axis === 'x' ? 'y' : 'x';
  }
  function getAxisLength(axis) {
    return axis === 'y' ? 'height' : 'width';
  }
  function getSideAxis(placement) {
    const firstChar = placement[0];
    return firstChar === 't' || firstChar === 'b' ? 'y' : 'x';
  }
  function getAlignmentAxis(placement) {
    return getOppositeAxis(getSideAxis(placement));
  }
  function getAlignmentSides(placement, rects, rtl) {
    if (rtl === void 0) {
      rtl = false;
    }
    const alignment = getAlignment(placement);
    const alignmentAxis = getAlignmentAxis(placement);
    const length = getAxisLength(alignmentAxis);
    let mainAlignmentSide = alignmentAxis === 'x' ? alignment === (rtl ? 'end' : 'start') ? 'right' : 'left' : alignment === 'start' ? 'bottom' : 'top';
    if (rects.reference[length] > rects.floating[length]) {
      mainAlignmentSide = getOppositePlacement(mainAlignmentSide);
    }
    return [mainAlignmentSide, getOppositePlacement(mainAlignmentSide)];
  }
  function getExpandedPlacements(placement) {
    const oppositePlacement = getOppositePlacement(placement);
    return [getOppositeAlignmentPlacement(placement), oppositePlacement, getOppositeAlignmentPlacement(oppositePlacement)];
  }
  function getOppositeAlignmentPlacement(placement) {
    return placement.includes('start') ? placement.replace('start', 'end') : placement.replace('end', 'start');
  }
  const lrPlacement = ['left', 'right'];
  const rlPlacement = ['right', 'left'];
  const tbPlacement = ['top', 'bottom'];
  const btPlacement = ['bottom', 'top'];
  function getSideList(side, isStart, rtl) {
    switch (side) {
      case 'top':
      case 'bottom':
        if (rtl) return isStart ? rlPlacement : lrPlacement;
        return isStart ? lrPlacement : rlPlacement;
      case 'left':
      case 'right':
        return isStart ? tbPlacement : btPlacement;
      default:
        return [];
    }
  }
  function getOppositeAxisPlacements(placement, flipAlignment, direction, rtl) {
    const alignment = getAlignment(placement);
    let list = getSideList(getSide(placement), direction === 'start', rtl);
    if (alignment) {
      list = list.map(side => side + "-" + alignment);
      if (flipAlignment) {
        list = list.concat(list.map(getOppositeAlignmentPlacement));
      }
    }
    return list;
  }
  function getOppositePlacement(placement) {
    const side = getSide(placement);
    return oppositeSideMap[side] + placement.slice(side.length);
  }
  function expandPaddingObject(padding) {
    return {
      top: 0,
      right: 0,
      bottom: 0,
      left: 0,
      ...padding
    };
  }
  function getPaddingObject(padding) {
    return typeof padding !== 'number' ? expandPaddingObject(padding) : {
      top: padding,
      right: padding,
      bottom: padding,
      left: padding
    };
  }
  function rectToClientRect(rect) {
    const {
      x,
      y,
      width,
      height
    } = rect;
    return {
      width,
      height,
      top: y,
      left: x,
      right: x + width,
      bottom: y + height,
      x,
      y
    };
  }

  function computeCoordsFromPlacement(_ref, placement, rtl) {
    let {
      reference,
      floating
    } = _ref;
    const sideAxis = getSideAxis(placement);
    const alignmentAxis = getAlignmentAxis(placement);
    const alignLength = getAxisLength(alignmentAxis);
    const side = getSide(placement);
    const isVertical = sideAxis === 'y';
    const commonX = reference.x + reference.width / 2 - floating.width / 2;
    const commonY = reference.y + reference.height / 2 - floating.height / 2;
    const commonAlign = reference[alignLength] / 2 - floating[alignLength] / 2;
    let coords;
    switch (side) {
      case 'top':
        coords = {
          x: commonX,
          y: reference.y - floating.height
        };
        break;
      case 'bottom':
        coords = {
          x: commonX,
          y: reference.y + reference.height
        };
        break;
      case 'right':
        coords = {
          x: reference.x + reference.width,
          y: commonY
        };
        break;
      case 'left':
        coords = {
          x: reference.x - floating.width,
          y: commonY
        };
        break;
      default:
        coords = {
          x: reference.x,
          y: reference.y
        };
    }
    switch (getAlignment(placement)) {
      case 'start':
        coords[alignmentAxis] -= commonAlign * (rtl && isVertical ? -1 : 1);
        break;
      case 'end':
        coords[alignmentAxis] += commonAlign * (rtl && isVertical ? -1 : 1);
        break;
    }
    return coords;
  }

  /**
   * Resolves with an object of overflow side offsets that determine how much the
   * element is overflowing a given clipping boundary on each side.
   * - positive = overflowing the boundary by that number of pixels
   * - negative = how many pixels left before it will overflow
   * - 0 = lies flush with the boundary
   * @see https://floating-ui.com/docs/detectOverflow
   */
  async function detectOverflow(state, options) {
    var _await$platform$isEle;
    if (options === void 0) {
      options = {};
    }
    const {
      x,
      y,
      platform,
      rects,
      elements,
      strategy
    } = state;
    const {
      boundary = 'clippingAncestors',
      rootBoundary = 'viewport',
      elementContext = 'floating',
      altBoundary = false,
      padding = 0
    } = evaluate(options, state);
    const paddingObject = getPaddingObject(padding);
    const altContext = elementContext === 'floating' ? 'reference' : 'floating';
    const element = elements[altBoundary ? altContext : elementContext];
    const clippingClientRect = rectToClientRect(await platform.getClippingRect({
      element: ((_await$platform$isEle = await (platform.isElement == null ? void 0 : platform.isElement(element))) != null ? _await$platform$isEle : true) ? element : element.contextElement || (await (platform.getDocumentElement == null ? void 0 : platform.getDocumentElement(elements.floating))),
      boundary,
      rootBoundary,
      strategy
    }));
    const rect = elementContext === 'floating' ? {
      x,
      y,
      width: rects.floating.width,
      height: rects.floating.height
    } : rects.reference;
    const offsetParent = await (platform.getOffsetParent == null ? void 0 : platform.getOffsetParent(elements.floating));
    const offsetScale = (await (platform.isElement == null ? void 0 : platform.isElement(offsetParent))) ? (await (platform.getScale == null ? void 0 : platform.getScale(offsetParent))) || {
      x: 1,
      y: 1
    } : {
      x: 1,
      y: 1
    };
    const elementClientRect = rectToClientRect(platform.convertOffsetParentRelativeRectToViewportRelativeRect ? await platform.convertOffsetParentRelativeRectToViewportRelativeRect({
      elements,
      rect,
      offsetParent,
      strategy
    }) : rect);
    return {
      top: (clippingClientRect.top - elementClientRect.top + paddingObject.top) / offsetScale.y,
      bottom: (elementClientRect.bottom - clippingClientRect.bottom + paddingObject.bottom) / offsetScale.y,
      left: (clippingClientRect.left - elementClientRect.left + paddingObject.left) / offsetScale.x,
      right: (elementClientRect.right - clippingClientRect.right + paddingObject.right) / offsetScale.x
    };
  }

  // Maximum number of resets that can occur before bailing to avoid infinite reset loops.
  const MAX_RESET_COUNT = 50;

  /**
   * Computes the `x` and `y` coordinates that will place the floating element
   * next to a given reference element.
   *
   * This export does not have any `platform` interface logic. You will need to
   * write one for the platform you are using Floating UI with.
   */
  const computePosition = async (reference, floating, config) => {
    const {
      placement = 'bottom',
      strategy = 'absolute',
      middleware = [],
      platform
    } = config;
    const platformWithDetectOverflow = platform.detectOverflow ? platform : {
      ...platform,
      detectOverflow
    };
    const rtl = await (platform.isRTL == null ? void 0 : platform.isRTL(floating));
    let rects = await platform.getElementRects({
      reference,
      floating,
      strategy
    });
    let {
      x,
      y
    } = computeCoordsFromPlacement(rects, placement, rtl);
    let statefulPlacement = placement;
    let resetCount = 0;
    const middlewareData = {};
    for (let i = 0; i < middleware.length; i++) {
      const currentMiddleware = middleware[i];
      if (!currentMiddleware) {
        continue;
      }
      const {
        name,
        fn
      } = currentMiddleware;
      const {
        x: nextX,
        y: nextY,
        data,
        reset
      } = await fn({
        x,
        y,
        initialPlacement: placement,
        placement: statefulPlacement,
        strategy,
        middlewareData,
        rects,
        platform: platformWithDetectOverflow,
        elements: {
          reference,
          floating
        }
      });
      x = nextX != null ? nextX : x;
      y = nextY != null ? nextY : y;
      middlewareData[name] = {
        ...middlewareData[name],
        ...data
      };
      if (reset && resetCount < MAX_RESET_COUNT) {
        resetCount++;
        if (typeof reset === 'object') {
          if (reset.placement) {
            statefulPlacement = reset.placement;
          }
          if (reset.rects) {
            rects = reset.rects === true ? await platform.getElementRects({
              reference,
              floating,
              strategy
            }) : reset.rects;
          }
          ({
            x,
            y
          } = computeCoordsFromPlacement(rects, statefulPlacement, rtl));
        }
        i = -1;
      }
    }
    return {
      x,
      y,
      placement: statefulPlacement,
      strategy,
      middlewareData
    };
  };

  /**
   * Provides data to position an inner element of the floating element so that it
   * appears centered to the reference element.
   * @see https://floating-ui.com/docs/arrow
   */
  const arrow = options => ({
    name: 'arrow',
    options,
    async fn(state) {
      const {
        x,
        y,
        placement,
        rects,
        platform,
        elements,
        middlewareData
      } = state;
      // Since `element` is required, we don't Partial<> the type.
      const {
        element,
        padding = 0
      } = evaluate(options, state) || {};
      if (element == null) {
        return {};
      }
      const paddingObject = getPaddingObject(padding);
      const coords = {
        x,
        y
      };
      const axis = getAlignmentAxis(placement);
      const length = getAxisLength(axis);
      const arrowDimensions = await platform.getDimensions(element);
      const isYAxis = axis === 'y';
      const minProp = isYAxis ? 'top' : 'left';
      const maxProp = isYAxis ? 'bottom' : 'right';
      const clientProp = isYAxis ? 'clientHeight' : 'clientWidth';
      const endDiff = rects.reference[length] + rects.reference[axis] - coords[axis] - rects.floating[length];
      const startDiff = coords[axis] - rects.reference[axis];
      const arrowOffsetParent = await (platform.getOffsetParent == null ? void 0 : platform.getOffsetParent(element));
      let clientSize = arrowOffsetParent ? arrowOffsetParent[clientProp] : 0;

      // DOM platform can return `window` as the `offsetParent`.
      if (!clientSize || !(await (platform.isElement == null ? void 0 : platform.isElement(arrowOffsetParent)))) {
        clientSize = elements.floating[clientProp] || rects.floating[length];
      }
      const centerToReference = endDiff / 2 - startDiff / 2;

      // If the padding is large enough that it causes the arrow to no longer be
      // centered, modify the padding so that it is centered.
      const largestPossiblePadding = clientSize / 2 - arrowDimensions[length] / 2 - 1;
      const minPadding = min(paddingObject[minProp], largestPossiblePadding);
      const maxPadding = min(paddingObject[maxProp], largestPossiblePadding);

      // Make sure the arrow doesn't overflow the floating element if the center
      // point is outside the floating element's bounds.
      const min$1 = minPadding;
      const max = clientSize - arrowDimensions[length] - maxPadding;
      const center = clientSize / 2 - arrowDimensions[length] / 2 + centerToReference;
      const offset = clamp(min$1, center, max);

      // If the reference is small enough that the arrow's padding causes it to
      // to point to nothing for an aligned placement, adjust the offset of the
      // floating element itself. To ensure `shift()` continues to take action,
      // a single reset is performed when this is true.
      const shouldAddOffset = !middlewareData.arrow && getAlignment(placement) != null && center !== offset && rects.reference[length] / 2 - (center < min$1 ? minPadding : maxPadding) - arrowDimensions[length] / 2 < 0;
      const alignmentOffset = shouldAddOffset ? center < min$1 ? center - min$1 : center - max : 0;
      return {
        [axis]: coords[axis] + alignmentOffset,
        data: {
          [axis]: offset,
          centerOffset: center - offset - alignmentOffset,
          ...(shouldAddOffset && {
            alignmentOffset
          })
        },
        reset: shouldAddOffset
      };
    }
  });

  function getPlacementList(alignment, autoAlignment, allowedPlacements) {
    const allowedPlacementsSortedByAlignment = alignment ? [...allowedPlacements.filter(placement => getAlignment(placement) === alignment), ...allowedPlacements.filter(placement => getAlignment(placement) !== alignment)] : allowedPlacements.filter(placement => getSide(placement) === placement);
    return allowedPlacementsSortedByAlignment.filter(placement => {
      if (alignment) {
        return getAlignment(placement) === alignment || (autoAlignment ? getOppositeAlignmentPlacement(placement) !== placement : false);
      }
      return true;
    });
  }
  /**
   * Optimizes the visibility of the floating element by choosing the placement
   * that has the most space available automatically, without needing to specify a
   * preferred placement. Alternative to `flip`.
   * @see https://floating-ui.com/docs/autoPlacement
   */
  const autoPlacement = function (options) {
    if (options === void 0) {
      options = {};
    }
    return {
      name: 'autoPlacement',
      options,
      async fn(state) {
        var _middlewareData$autoP, _middlewareData$autoP2, _placementsThatFitOnE;
        const {
          rects,
          middlewareData,
          placement,
          platform,
          elements
        } = state;
        const {
          crossAxis = false,
          alignment,
          allowedPlacements = placements,
          autoAlignment = true,
          ...detectOverflowOptions
        } = evaluate(options, state);
        const placements$1 = alignment !== undefined || allowedPlacements === placements ? getPlacementList(alignment || null, autoAlignment, allowedPlacements) : allowedPlacements;
        const overflow = await platform.detectOverflow(state, detectOverflowOptions);
        const currentIndex = ((_middlewareData$autoP = middlewareData.autoPlacement) == null ? void 0 : _middlewareData$autoP.index) || 0;
        const currentPlacement = placements$1[currentIndex];
        if (currentPlacement == null) {
          return {};
        }
        const alignmentSides = getAlignmentSides(currentPlacement, rects, await (platform.isRTL == null ? void 0 : platform.isRTL(elements.floating)));

        // Make `computeCoords` start from the right place.
        if (placement !== currentPlacement) {
          return {
            reset: {
              placement: placements$1[0]
            }
          };
        }
        const currentOverflows = [overflow[getSide(currentPlacement)], overflow[alignmentSides[0]], overflow[alignmentSides[1]]];
        const allOverflows = [...(((_middlewareData$autoP2 = middlewareData.autoPlacement) == null ? void 0 : _middlewareData$autoP2.overflows) || []), {
          placement: currentPlacement,
          overflows: currentOverflows
        }];
        const nextPlacement = placements$1[currentIndex + 1];

        // There are more placements to check.
        if (nextPlacement) {
          return {
            data: {
              index: currentIndex + 1,
              overflows: allOverflows
            },
            reset: {
              placement: nextPlacement
            }
          };
        }
        const placementsSortedByMostSpace = allOverflows.map(d => {
          const alignment = getAlignment(d.placement);
          return [d.placement, alignment && crossAxis ?
          // Check along the mainAxis and main crossAxis side.
          d.overflows.slice(0, 2).reduce((acc, v) => acc + v, 0) :
          // Check only the mainAxis.
          d.overflows[0], d.overflows];
        }).sort((a, b) => a[1] - b[1]);
        const placementsThatFitOnEachSide = placementsSortedByMostSpace.filter(d => d[2].slice(0,
        // Aligned placements should not check their opposite crossAxis
        // side.
        getAlignment(d[0]) ? 2 : 3).every(v => v <= 0));
        const resetPlacement = ((_placementsThatFitOnE = placementsThatFitOnEachSide[0]) == null ? void 0 : _placementsThatFitOnE[0]) || placementsSortedByMostSpace[0][0];
        if (resetPlacement !== placement) {
          return {
            data: {
              index: currentIndex + 1,
              overflows: allOverflows
            },
            reset: {
              placement: resetPlacement
            }
          };
        }
        return {};
      }
    };
  };

  /**
   * Optimizes the visibility of the floating element by flipping the `placement`
   * in order to keep it in view when the preferred placement(s) will overflow the
   * clipping boundary. Alternative to `autoPlacement`.
   * @see https://floating-ui.com/docs/flip
   */
  const flip = function (options) {
    if (options === void 0) {
      options = {};
    }
    return {
      name: 'flip',
      options,
      async fn(state) {
        var _middlewareData$arrow, _middlewareData$flip;
        const {
          placement,
          middlewareData,
          rects,
          initialPlacement,
          platform,
          elements
        } = state;
        const {
          mainAxis: checkMainAxis = true,
          crossAxis: checkCrossAxis = true,
          fallbackPlacements: specifiedFallbackPlacements,
          fallbackStrategy = 'bestFit',
          fallbackAxisSideDirection = 'none',
          flipAlignment = true,
          ...detectOverflowOptions
        } = evaluate(options, state);

        // If a reset by the arrow was caused due to an alignment offset being
        // added, we should skip any logic now since `flip()` has already done its
        // work.
        // https://github.com/floating-ui/floating-ui/issues/2549#issuecomment-1719601643
        if ((_middlewareData$arrow = middlewareData.arrow) != null && _middlewareData$arrow.alignmentOffset) {
          return {};
        }
        const side = getSide(placement);
        const initialSideAxis = getSideAxis(initialPlacement);
        const isBasePlacement = getSide(initialPlacement) === initialPlacement;
        const rtl = await (platform.isRTL == null ? void 0 : platform.isRTL(elements.floating));
        const fallbackPlacements = specifiedFallbackPlacements || (isBasePlacement || !flipAlignment ? [getOppositePlacement(initialPlacement)] : getExpandedPlacements(initialPlacement));
        const hasFallbackAxisSideDirection = fallbackAxisSideDirection !== 'none';
        if (!specifiedFallbackPlacements && hasFallbackAxisSideDirection) {
          fallbackPlacements.push(...getOppositeAxisPlacements(initialPlacement, flipAlignment, fallbackAxisSideDirection, rtl));
        }
        const placements = [initialPlacement, ...fallbackPlacements];
        const overflow = await platform.detectOverflow(state, detectOverflowOptions);
        const overflows = [];
        let overflowsData = ((_middlewareData$flip = middlewareData.flip) == null ? void 0 : _middlewareData$flip.overflows) || [];
        if (checkMainAxis) {
          overflows.push(overflow[side]);
        }
        if (checkCrossAxis) {
          const sides = getAlignmentSides(placement, rects, rtl);
          overflows.push(overflow[sides[0]], overflow[sides[1]]);
        }
        overflowsData = [...overflowsData, {
          placement,
          overflows
        }];

        // One or more sides is overflowing.
        if (!overflows.every(side => side <= 0)) {
          var _middlewareData$flip2, _overflowsData$filter;
          const nextIndex = (((_middlewareData$flip2 = middlewareData.flip) == null ? void 0 : _middlewareData$flip2.index) || 0) + 1;
          const nextPlacement = placements[nextIndex];
          if (nextPlacement) {
            const ignoreCrossAxisOverflow = checkCrossAxis === 'alignment' ? initialSideAxis !== getSideAxis(nextPlacement) : false;
            if (!ignoreCrossAxisOverflow ||
            // We leave the current main axis only if every placement on that axis
            // overflows the main axis.
            overflowsData.every(d => getSideAxis(d.placement) === initialSideAxis ? d.overflows[0] > 0 : true)) {
              // Try next placement and re-run the lifecycle.
              return {
                data: {
                  index: nextIndex,
                  overflows: overflowsData
                },
                reset: {
                  placement: nextPlacement
                }
              };
            }
          }

          // First, find the candidates that fit on the mainAxis side of overflow,
          // then find the placement that fits the best on the main crossAxis side.
          let resetPlacement = (_overflowsData$filter = overflowsData.filter(d => d.overflows[0] <= 0).sort((a, b) => a.overflows[1] - b.overflows[1])[0]) == null ? void 0 : _overflowsData$filter.placement;

          // Otherwise fallback.
          if (!resetPlacement) {
            switch (fallbackStrategy) {
              case 'bestFit':
                {
                  var _overflowsData$filter2;
                  const placement = (_overflowsData$filter2 = overflowsData.filter(d => {
                    if (hasFallbackAxisSideDirection) {
                      const currentSideAxis = getSideAxis(d.placement);
                      return currentSideAxis === initialSideAxis ||
                      // Create a bias to the `y` side axis due to horizontal
                      // reading directions favoring greater width.
                      currentSideAxis === 'y';
                    }
                    return true;
                  }).map(d => [d.placement, d.overflows.filter(overflow => overflow > 0).reduce((acc, overflow) => acc + overflow, 0)]).sort((a, b) => a[1] - b[1])[0]) == null ? void 0 : _overflowsData$filter2[0];
                  if (placement) {
                    resetPlacement = placement;
                  }
                  break;
                }
              case 'initialPlacement':
                resetPlacement = initialPlacement;
                break;
            }
          }
          if (placement !== resetPlacement) {
            return {
              reset: {
                placement: resetPlacement
              }
            };
          }
        }
        return {};
      }
    };
  };

  function getSideOffsets(overflow, rect) {
    return {
      top: overflow.top - rect.height,
      right: overflow.right - rect.width,
      bottom: overflow.bottom - rect.height,
      left: overflow.left - rect.width
    };
  }
  function isAnySideFullyClipped(overflow) {
    return sides.some(side => overflow[side] >= 0);
  }
  /**
   * Provides data to hide the floating element in applicable situations, such as
   * when it is not in the same clipping context as the reference element.
   * @see https://floating-ui.com/docs/hide
   */
  const hide = function (options) {
    if (options === void 0) {
      options = {};
    }
    return {
      name: 'hide',
      options,
      async fn(state) {
        const {
          rects,
          platform
        } = state;
        const {
          strategy = 'referenceHidden',
          ...detectOverflowOptions
        } = evaluate(options, state);
        switch (strategy) {
          case 'referenceHidden':
            {
              const overflow = await platform.detectOverflow(state, {
                ...detectOverflowOptions,
                elementContext: 'reference'
              });
              const offsets = getSideOffsets(overflow, rects.reference);
              return {
                data: {
                  referenceHiddenOffsets: offsets,
                  referenceHidden: isAnySideFullyClipped(offsets)
                }
              };
            }
          case 'escaped':
            {
              const overflow = await platform.detectOverflow(state, {
                ...detectOverflowOptions,
                altBoundary: true
              });
              const offsets = getSideOffsets(overflow, rects.floating);
              return {
                data: {
                  escapedOffsets: offsets,
                  escaped: isAnySideFullyClipped(offsets)
                }
              };
            }
          default:
            {
              return {};
            }
        }
      }
    };
  };

  function getBoundingRect(rects) {
    const minX = min(...rects.map(rect => rect.left));
    const minY = min(...rects.map(rect => rect.top));
    const maxX = max(...rects.map(rect => rect.right));
    const maxY = max(...rects.map(rect => rect.bottom));
    return {
      x: minX,
      y: minY,
      width: maxX - minX,
      height: maxY - minY
    };
  }
  function getRectsByLine(rects) {
    const sortedRects = rects.slice().sort((a, b) => a.y - b.y);
    const groups = [];
    let prevRect = null;
    for (let i = 0; i < sortedRects.length; i++) {
      const rect = sortedRects[i];
      if (!prevRect || rect.y - prevRect.y > prevRect.height / 2) {
        groups.push([rect]);
      } else {
        groups[groups.length - 1].push(rect);
      }
      prevRect = rect;
    }
    return groups.map(rect => rectToClientRect(getBoundingRect(rect)));
  }
  /**
   * Provides improved positioning for inline reference elements that can span
   * over multiple lines, such as hyperlinks or range selections.
   * @see https://floating-ui.com/docs/inline
   */
  const inline = function (options) {
    if (options === void 0) {
      options = {};
    }
    return {
      name: 'inline',
      options,
      async fn(state) {
        const {
          placement,
          elements,
          rects,
          platform,
          strategy
        } = state;
        // A MouseEvent's client{X,Y} coords can be up to 2 pixels off a
        // ClientRect's bounds, despite the event listener being triggered. A
        // padding of 2 seems to handle this issue.
        const {
          padding = 2,
          x,
          y
        } = evaluate(options, state);
        const nativeClientRects = Array.from((await (platform.getClientRects == null ? void 0 : platform.getClientRects(elements.reference))) || []);
        const clientRects = getRectsByLine(nativeClientRects);
        const fallback = rectToClientRect(getBoundingRect(nativeClientRects));
        const paddingObject = getPaddingObject(padding);
        function getBoundingClientRect() {
          // There are two rects and they are disjoined.
          if (clientRects.length === 2 && clientRects[0].left > clientRects[1].right && x != null && y != null) {
            // Find the first rect in which the point is fully inside.
            return clientRects.find(rect => x > rect.left - paddingObject.left && x < rect.right + paddingObject.right && y > rect.top - paddingObject.top && y < rect.bottom + paddingObject.bottom) || fallback;
          }

          // There are 2 or more connected rects.
          if (clientRects.length >= 2) {
            if (getSideAxis(placement) === 'y') {
              const firstRect = clientRects[0];
              const lastRect = clientRects[clientRects.length - 1];
              const isTop = getSide(placement) === 'top';
              const top = firstRect.top;
              const bottom = lastRect.bottom;
              const left = isTop ? firstRect.left : lastRect.left;
              const right = isTop ? firstRect.right : lastRect.right;
              const width = right - left;
              const height = bottom - top;
              return {
                top,
                bottom,
                left,
                right,
                width,
                height,
                x: left,
                y: top
              };
            }
            const isLeftSide = getSide(placement) === 'left';
            const maxRight = max(...clientRects.map(rect => rect.right));
            const minLeft = min(...clientRects.map(rect => rect.left));
            const measureRects = clientRects.filter(rect => isLeftSide ? rect.left === minLeft : rect.right === maxRight);
            const top = measureRects[0].top;
            const bottom = measureRects[measureRects.length - 1].bottom;
            const left = minLeft;
            const right = maxRight;
            const width = right - left;
            const height = bottom - top;
            return {
              top,
              bottom,
              left,
              right,
              width,
              height,
              x: left,
              y: top
            };
          }
          return fallback;
        }
        const resetRects = await platform.getElementRects({
          reference: {
            getBoundingClientRect
          },
          floating: elements.floating,
          strategy
        });
        if (rects.reference.x !== resetRects.reference.x || rects.reference.y !== resetRects.reference.y || rects.reference.width !== resetRects.reference.width || rects.reference.height !== resetRects.reference.height) {
          return {
            reset: {
              rects: resetRects
            }
          };
        }
        return {};
      }
    };
  };

  const originSides = /*#__PURE__*/new Set(['left', 'top']);

  // For type backwards-compatibility, the `OffsetOptions` type was also
  // Derivable.

  async function convertValueToCoords(state, options) {
    const {
      placement,
      platform,
      elements
    } = state;
    const rtl = await (platform.isRTL == null ? void 0 : platform.isRTL(elements.floating));
    const side = getSide(placement);
    const alignment = getAlignment(placement);
    const isVertical = getSideAxis(placement) === 'y';
    const mainAxisMulti = originSides.has(side) ? -1 : 1;
    const crossAxisMulti = rtl && isVertical ? -1 : 1;
    const rawValue = evaluate(options, state);

    // eslint-disable-next-line prefer-const
    let {
      mainAxis,
      crossAxis,
      alignmentAxis
    } = typeof rawValue === 'number' ? {
      mainAxis: rawValue,
      crossAxis: 0,
      alignmentAxis: null
    } : {
      mainAxis: rawValue.mainAxis || 0,
      crossAxis: rawValue.crossAxis || 0,
      alignmentAxis: rawValue.alignmentAxis
    };
    if (alignment && typeof alignmentAxis === 'number') {
      crossAxis = alignment === 'end' ? alignmentAxis * -1 : alignmentAxis;
    }
    return isVertical ? {
      x: crossAxis * crossAxisMulti,
      y: mainAxis * mainAxisMulti
    } : {
      x: mainAxis * mainAxisMulti,
      y: crossAxis * crossAxisMulti
    };
  }

  /**
   * Modifies the placement by translating the floating element along the
   * specified axes.
   * A number (shorthand for `mainAxis` or distance), or an axes configuration
   * object may be passed.
   * @see https://floating-ui.com/docs/offset
   */
  const offset = function (options) {
    if (options === void 0) {
      options = 0;
    }
    return {
      name: 'offset',
      options,
      async fn(state) {
        var _middlewareData$offse, _middlewareData$arrow;
        const {
          x,
          y,
          placement,
          middlewareData
        } = state;
        const diffCoords = await convertValueToCoords(state, options);

        // If the placement is the same and the arrow caused an alignment offset
        // then we don't need to change the positioning coordinates.
        if (placement === ((_middlewareData$offse = middlewareData.offset) == null ? void 0 : _middlewareData$offse.placement) && (_middlewareData$arrow = middlewareData.arrow) != null && _middlewareData$arrow.alignmentOffset) {
          return {};
        }
        return {
          x: x + diffCoords.x,
          y: y + diffCoords.y,
          data: {
            ...diffCoords,
            placement
          }
        };
      }
    };
  };

  /**
   * Optimizes the visibility of the floating element by shifting it in order to
   * keep it in view when it will overflow the clipping boundary.
   * @see https://floating-ui.com/docs/shift
   */
  const shift = function (options) {
    if (options === void 0) {
      options = {};
    }
    return {
      name: 'shift',
      options,
      async fn(state) {
        const {
          x,
          y,
          placement,
          platform
        } = state;
        const {
          mainAxis: checkMainAxis = true,
          crossAxis: checkCrossAxis = false,
          limiter = {
            fn: _ref => {
              let {
                x,
                y
              } = _ref;
              return {
                x,
                y
              };
            }
          },
          ...detectOverflowOptions
        } = evaluate(options, state);
        const coords = {
          x,
          y
        };
        const overflow = await platform.detectOverflow(state, detectOverflowOptions);
        const crossAxis = getSideAxis(getSide(placement));
        const mainAxis = getOppositeAxis(crossAxis);
        let mainAxisCoord = coords[mainAxis];
        let crossAxisCoord = coords[crossAxis];
        if (checkMainAxis) {
          const minSide = mainAxis === 'y' ? 'top' : 'left';
          const maxSide = mainAxis === 'y' ? 'bottom' : 'right';
          const min = mainAxisCoord + overflow[minSide];
          const max = mainAxisCoord - overflow[maxSide];
          mainAxisCoord = clamp(min, mainAxisCoord, max);
        }
        if (checkCrossAxis) {
          const minSide = crossAxis === 'y' ? 'top' : 'left';
          const maxSide = crossAxis === 'y' ? 'bottom' : 'right';
          const min = crossAxisCoord + overflow[minSide];
          const max = crossAxisCoord - overflow[maxSide];
          crossAxisCoord = clamp(min, crossAxisCoord, max);
        }
        const limitedCoords = limiter.fn({
          ...state,
          [mainAxis]: mainAxisCoord,
          [crossAxis]: crossAxisCoord
        });
        return {
          ...limitedCoords,
          data: {
            x: limitedCoords.x - x,
            y: limitedCoords.y - y,
            enabled: {
              [mainAxis]: checkMainAxis,
              [crossAxis]: checkCrossAxis
            }
          }
        };
      }
    };
  };
  /**
   * Built-in `limiter` that will stop `shift()` at a certain point.
   */
  const limitShift = function (options) {
    if (options === void 0) {
      options = {};
    }
    return {
      options,
      fn(state) {
        const {
          x,
          y,
          placement,
          rects,
          middlewareData
        } = state;
        const {
          offset = 0,
          mainAxis: checkMainAxis = true,
          crossAxis: checkCrossAxis = true
        } = evaluate(options, state);
        const coords = {
          x,
          y
        };
        const crossAxis = getSideAxis(placement);
        const mainAxis = getOppositeAxis(crossAxis);
        let mainAxisCoord = coords[mainAxis];
        let crossAxisCoord = coords[crossAxis];
        const rawOffset = evaluate(offset, state);
        const computedOffset = typeof rawOffset === 'number' ? {
          mainAxis: rawOffset,
          crossAxis: 0
        } : {
          mainAxis: 0,
          crossAxis: 0,
          ...rawOffset
        };
        if (checkMainAxis) {
          const len = mainAxis === 'y' ? 'height' : 'width';
          const limitMin = rects.reference[mainAxis] - rects.floating[len] + computedOffset.mainAxis;
          const limitMax = rects.reference[mainAxis] + rects.reference[len] - computedOffset.mainAxis;
          if (mainAxisCoord < limitMin) {
            mainAxisCoord = limitMin;
          } else if (mainAxisCoord > limitMax) {
            mainAxisCoord = limitMax;
          }
        }
        if (checkCrossAxis) {
          var _middlewareData$offse, _middlewareData$offse2;
          const len = mainAxis === 'y' ? 'width' : 'height';
          const isOriginSide = originSides.has(getSide(placement));
          const limitMin = rects.reference[crossAxis] - rects.floating[len] + (isOriginSide ? ((_middlewareData$offse = middlewareData.offset) == null ? void 0 : _middlewareData$offse[crossAxis]) || 0 : 0) + (isOriginSide ? 0 : computedOffset.crossAxis);
          const limitMax = rects.reference[crossAxis] + rects.reference[len] + (isOriginSide ? 0 : ((_middlewareData$offse2 = middlewareData.offset) == null ? void 0 : _middlewareData$offse2[crossAxis]) || 0) - (isOriginSide ? computedOffset.crossAxis : 0);
          if (crossAxisCoord < limitMin) {
            crossAxisCoord = limitMin;
          } else if (crossAxisCoord > limitMax) {
            crossAxisCoord = limitMax;
          }
        }
        return {
          [mainAxis]: mainAxisCoord,
          [crossAxis]: crossAxisCoord
        };
      }
    };
  };

  /**
   * Provides data that allows you to change the size of the floating element —
   * for instance, prevent it from overflowing the clipping boundary or match the
   * width of the reference element.
   * @see https://floating-ui.com/docs/size
   */
  const size = function (options) {
    if (options === void 0) {
      options = {};
    }
    return {
      name: 'size',
      options,
      async fn(state) {
        var _state$middlewareData, _state$middlewareData2;
        const {
          placement,
          rects,
          platform,
          elements
        } = state;
        const {
          apply = () => {},
          ...detectOverflowOptions
        } = evaluate(options, state);
        const overflow = await platform.detectOverflow(state, detectOverflowOptions);
        const side = getSide(placement);
        const alignment = getAlignment(placement);
        const isYAxis = getSideAxis(placement) === 'y';
        const {
          width,
          height
        } = rects.floating;
        let heightSide;
        let widthSide;
        if (side === 'top' || side === 'bottom') {
          heightSide = side;
          widthSide = alignment === ((await (platform.isRTL == null ? void 0 : platform.isRTL(elements.floating))) ? 'start' : 'end') ? 'left' : 'right';
        } else {
          widthSide = side;
          heightSide = alignment === 'end' ? 'top' : 'bottom';
        }
        const maximumClippingHeight = height - overflow.top - overflow.bottom;
        const maximumClippingWidth = width - overflow.left - overflow.right;
        const overflowAvailableHeight = min(height - overflow[heightSide], maximumClippingHeight);
        const overflowAvailableWidth = min(width - overflow[widthSide], maximumClippingWidth);
        const noShift = !state.middlewareData.shift;
        let availableHeight = overflowAvailableHeight;
        let availableWidth = overflowAvailableWidth;
        if ((_state$middlewareData = state.middlewareData.shift) != null && _state$middlewareData.enabled.x) {
          availableWidth = maximumClippingWidth;
        }
        if ((_state$middlewareData2 = state.middlewareData.shift) != null && _state$middlewareData2.enabled.y) {
          availableHeight = maximumClippingHeight;
        }
        if (noShift && !alignment) {
          const xMin = max(overflow.left, 0);
          const xMax = max(overflow.right, 0);
          const yMin = max(overflow.top, 0);
          const yMax = max(overflow.bottom, 0);
          if (isYAxis) {
            availableWidth = width - 2 * (xMin !== 0 || xMax !== 0 ? xMin + xMax : max(overflow.left, overflow.right));
          } else {
            availableHeight = height - 2 * (yMin !== 0 || yMax !== 0 ? yMin + yMax : max(overflow.top, overflow.bottom));
          }
        }
        await apply({
          ...state,
          availableWidth,
          availableHeight
        });
        const nextDimensions = await platform.getDimensions(elements.floating);
        if (width !== nextDimensions.width || height !== nextDimensions.height) {
          return {
            reset: {
              rects: true
            }
          };
        }
        return {};
      }
    };
  };

  exports.arrow = arrow;
  exports.autoPlacement = autoPlacement;
  exports.computePosition = computePosition;
  exports.detectOverflow = detectOverflow;
  exports.flip = flip;
  exports.hide = hide;
  exports.inline = inline;
  exports.limitShift = limitShift;
  exports.offset = offset;
  exports.rectToClientRect = rectToClientRect;
  exports.shift = shift;
  exports.size = size;

}));

// components/floatingui/floating_ui_dom.js
// @floating-ui/dom 1.7.6, dist/floating-ui.dom.umd.js, the version @base-ui/react
// 1.6.0 resolves at the pin in plans/UPSTREAM.md.
(function (global, factory) {
  typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports, require('@floating-ui/core')) :
  typeof define === 'function' && define.amd ? define(['exports', '@floating-ui/core'], factory) :
  (global = typeof globalThis !== 'undefined' ? globalThis : global || self, factory(global.FloatingUIDOM = {}, global.FloatingUICore));
})(this, (function (exports, core) { 'use strict';

  /**
   * Custom positioning reference element.
   * @see https://floating-ui.com/docs/virtual-elements
   */

  const min = Math.min;
  const max = Math.max;
  const round = Math.round;
  const floor = Math.floor;
  const createCoords = v => ({
    x: v,
    y: v
  });

  function hasWindow() {
    return typeof window !== 'undefined';
  }
  function getNodeName(node) {
    if (isNode(node)) {
      return (node.nodeName || '').toLowerCase();
    }
    // Mocked nodes in testing environments may not be instances of Node. By
    // returning `#document` an infinite loop won't occur.
    // https://github.com/floating-ui/floating-ui/issues/2317
    return '#document';
  }
  function getWindow(node) {
    var _node$ownerDocument;
    return (node == null || (_node$ownerDocument = node.ownerDocument) == null ? void 0 : _node$ownerDocument.defaultView) || window;
  }
  function getDocumentElement(node) {
    var _ref;
    return (_ref = (isNode(node) ? node.ownerDocument : node.document) || window.document) == null ? void 0 : _ref.documentElement;
  }
  function isNode(value) {
    if (!hasWindow()) {
      return false;
    }
    return value instanceof Node || value instanceof getWindow(value).Node;
  }
  function isElement(value) {
    if (!hasWindow()) {
      return false;
    }
    return value instanceof Element || value instanceof getWindow(value).Element;
  }
  function isHTMLElement(value) {
    if (!hasWindow()) {
      return false;
    }
    return value instanceof HTMLElement || value instanceof getWindow(value).HTMLElement;
  }
  function isShadowRoot(value) {
    if (!hasWindow() || typeof ShadowRoot === 'undefined') {
      return false;
    }
    return value instanceof ShadowRoot || value instanceof getWindow(value).ShadowRoot;
  }
  function isOverflowElement(element) {
    const {
      overflow,
      overflowX,
      overflowY,
      display
    } = getComputedStyle$1(element);
    return /auto|scroll|overlay|hidden|clip/.test(overflow + overflowY + overflowX) && display !== 'inline' && display !== 'contents';
  }
  function isTableElement(element) {
    return /^(table|td|th)$/.test(getNodeName(element));
  }
  function isTopLayer(element) {
    try {
      if (element.matches(':popover-open')) {
        return true;
      }
    } catch (_e) {
      // no-op
    }
    try {
      return element.matches(':modal');
    } catch (_e) {
      return false;
    }
  }
  const willChangeRe = /transform|translate|scale|rotate|perspective|filter/;
  const containRe = /paint|layout|strict|content/;
  const isNotNone = value => !!value && value !== 'none';
  let isWebKitValue;
  function isContainingBlock(elementOrCss) {
    const css = isElement(elementOrCss) ? getComputedStyle$1(elementOrCss) : elementOrCss;

    // https://developer.mozilla.org/en-US/docs/Web/CSS/Containing_block#identifying_the_containing_block
    // https://drafts.csswg.org/css-transforms-2/#individual-transforms
    return isNotNone(css.transform) || isNotNone(css.translate) || isNotNone(css.scale) || isNotNone(css.rotate) || isNotNone(css.perspective) || !isWebKit() && (isNotNone(css.backdropFilter) || isNotNone(css.filter)) || willChangeRe.test(css.willChange || '') || containRe.test(css.contain || '');
  }
  function getContainingBlock(element) {
    let currentNode = getParentNode(element);
    while (isHTMLElement(currentNode) && !isLastTraversableNode(currentNode)) {
      if (isContainingBlock(currentNode)) {
        return currentNode;
      } else if (isTopLayer(currentNode)) {
        return null;
      }
      currentNode = getParentNode(currentNode);
    }
    return null;
  }
  function isWebKit() {
    if (isWebKitValue == null) {
      isWebKitValue = typeof CSS !== 'undefined' && CSS.supports && CSS.supports('-webkit-backdrop-filter', 'none');
    }
    return isWebKitValue;
  }
  function isLastTraversableNode(node) {
    return /^(html|body|#document)$/.test(getNodeName(node));
  }
  function getComputedStyle$1(element) {
    return getWindow(element).getComputedStyle(element);
  }
  function getNodeScroll(element) {
    if (isElement(element)) {
      return {
        scrollLeft: element.scrollLeft,
        scrollTop: element.scrollTop
      };
    }
    return {
      scrollLeft: element.scrollX,
      scrollTop: element.scrollY
    };
  }
  function getParentNode(node) {
    if (getNodeName(node) === 'html') {
      return node;
    }
    const result =
    // Step into the shadow DOM of the parent of a slotted node.
    node.assignedSlot ||
    // DOM Element detected.
    node.parentNode ||
    // ShadowRoot detected.
    isShadowRoot(node) && node.host ||
    // Fallback.
    getDocumentElement(node);
    return isShadowRoot(result) ? result.host : result;
  }
  function getNearestOverflowAncestor(node) {
    const parentNode = getParentNode(node);
    if (isLastTraversableNode(parentNode)) {
      return node.ownerDocument ? node.ownerDocument.body : node.body;
    }
    if (isHTMLElement(parentNode) && isOverflowElement(parentNode)) {
      return parentNode;
    }
    return getNearestOverflowAncestor(parentNode);
  }
  function getOverflowAncestors(node, list, traverseIframes) {
    var _node$ownerDocument2;
    if (list === void 0) {
      list = [];
    }
    if (traverseIframes === void 0) {
      traverseIframes = true;
    }
    const scrollableAncestor = getNearestOverflowAncestor(node);
    const isBody = scrollableAncestor === ((_node$ownerDocument2 = node.ownerDocument) == null ? void 0 : _node$ownerDocument2.body);
    const win = getWindow(scrollableAncestor);
    if (isBody) {
      const frameElement = getFrameElement(win);
      return list.concat(win, win.visualViewport || [], isOverflowElement(scrollableAncestor) ? scrollableAncestor : [], frameElement && traverseIframes ? getOverflowAncestors(frameElement) : []);
    } else {
      return list.concat(scrollableAncestor, getOverflowAncestors(scrollableAncestor, [], traverseIframes));
    }
  }
  function getFrameElement(win) {
    return win.parent && Object.getPrototypeOf(win.parent) ? win.frameElement : null;
  }

  function getCssDimensions(element) {
    const css = getComputedStyle$1(element);
    // In testing environments, the `width` and `height` properties are empty
    // strings for SVG elements, returning NaN. Fallback to `0` in this case.
    let width = parseFloat(css.width) || 0;
    let height = parseFloat(css.height) || 0;
    const hasOffset = isHTMLElement(element);
    const offsetWidth = hasOffset ? element.offsetWidth : width;
    const offsetHeight = hasOffset ? element.offsetHeight : height;
    const shouldFallback = round(width) !== offsetWidth || round(height) !== offsetHeight;
    if (shouldFallback) {
      width = offsetWidth;
      height = offsetHeight;
    }
    return {
      width,
      height,
      $: shouldFallback
    };
  }

  function unwrapElement(element) {
    return !isElement(element) ? element.contextElement : element;
  }

  function getScale(element) {
    const domElement = unwrapElement(element);
    if (!isHTMLElement(domElement)) {
      return createCoords(1);
    }
    const rect = domElement.getBoundingClientRect();
    const {
      width,
      height,
      $
    } = getCssDimensions(domElement);
    let x = ($ ? round(rect.width) : rect.width) / width;
    let y = ($ ? round(rect.height) : rect.height) / height;

    // 0, NaN, or Infinity should always fallback to 1.

    if (!x || !Number.isFinite(x)) {
      x = 1;
    }
    if (!y || !Number.isFinite(y)) {
      y = 1;
    }
    return {
      x,
      y
    };
  }

  const noOffsets = /*#__PURE__*/createCoords(0);
  function getVisualOffsets(element) {
    const win = getWindow(element);
    if (!isWebKit() || !win.visualViewport) {
      return noOffsets;
    }
    return {
      x: win.visualViewport.offsetLeft,
      y: win.visualViewport.offsetTop
    };
  }
  function shouldAddVisualOffsets(element, isFixed, floatingOffsetParent) {
    if (isFixed === void 0) {
      isFixed = false;
    }
    if (!floatingOffsetParent || isFixed && floatingOffsetParent !== getWindow(element)) {
      return false;
    }
    return isFixed;
  }

  function getBoundingClientRect(element, includeScale, isFixedStrategy, offsetParent) {
    if (includeScale === void 0) {
      includeScale = false;
    }
    if (isFixedStrategy === void 0) {
      isFixedStrategy = false;
    }
    const clientRect = element.getBoundingClientRect();
    const domElement = unwrapElement(element);
    let scale = createCoords(1);
    if (includeScale) {
      if (offsetParent) {
        if (isElement(offsetParent)) {
          scale = getScale(offsetParent);
        }
      } else {
        scale = getScale(element);
      }
    }
    const visualOffsets = shouldAddVisualOffsets(domElement, isFixedStrategy, offsetParent) ? getVisualOffsets(domElement) : createCoords(0);
    let x = (clientRect.left + visualOffsets.x) / scale.x;
    let y = (clientRect.top + visualOffsets.y) / scale.y;
    let width = clientRect.width / scale.x;
    let height = clientRect.height / scale.y;
    if (domElement) {
      const win = getWindow(domElement);
      const offsetWin = offsetParent && isElement(offsetParent) ? getWindow(offsetParent) : offsetParent;
      let currentWin = win;
      let currentIFrame = getFrameElement(currentWin);
      while (currentIFrame && offsetParent && offsetWin !== currentWin) {
        const iframeScale = getScale(currentIFrame);
        const iframeRect = currentIFrame.getBoundingClientRect();
        const css = getComputedStyle$1(currentIFrame);
        const left = iframeRect.left + (currentIFrame.clientLeft + parseFloat(css.paddingLeft)) * iframeScale.x;
        const top = iframeRect.top + (currentIFrame.clientTop + parseFloat(css.paddingTop)) * iframeScale.y;
        x *= iframeScale.x;
        y *= iframeScale.y;
        width *= iframeScale.x;
        height *= iframeScale.y;
        x += left;
        y += top;
        currentWin = getWindow(currentIFrame);
        currentIFrame = getFrameElement(currentWin);
      }
    }
    return core.rectToClientRect({
      width,
      height,
      x,
      y
    });
  }

  // If <html> has a CSS width greater than the viewport, then this will be
  // incorrect for RTL.
  function getWindowScrollBarX(element, rect) {
    const leftScroll = getNodeScroll(element).scrollLeft;
    if (!rect) {
      return getBoundingClientRect(getDocumentElement(element)).left + leftScroll;
    }
    return rect.left + leftScroll;
  }

  function getHTMLOffset(documentElement, scroll) {
    const htmlRect = documentElement.getBoundingClientRect();
    const x = htmlRect.left + scroll.scrollLeft - getWindowScrollBarX(documentElement, htmlRect);
    const y = htmlRect.top + scroll.scrollTop;
    return {
      x,
      y
    };
  }

  function convertOffsetParentRelativeRectToViewportRelativeRect(_ref) {
    let {
      elements,
      rect,
      offsetParent,
      strategy
    } = _ref;
    const isFixed = strategy === 'fixed';
    const documentElement = getDocumentElement(offsetParent);
    const topLayer = elements ? isTopLayer(elements.floating) : false;
    if (offsetParent === documentElement || topLayer && isFixed) {
      return rect;
    }
    let scroll = {
      scrollLeft: 0,
      scrollTop: 0
    };
    let scale = createCoords(1);
    const offsets = createCoords(0);
    const isOffsetParentAnElement = isHTMLElement(offsetParent);
    if (isOffsetParentAnElement || !isOffsetParentAnElement && !isFixed) {
      if (getNodeName(offsetParent) !== 'body' || isOverflowElement(documentElement)) {
        scroll = getNodeScroll(offsetParent);
      }
      if (isOffsetParentAnElement) {
        const offsetRect = getBoundingClientRect(offsetParent);
        scale = getScale(offsetParent);
        offsets.x = offsetRect.x + offsetParent.clientLeft;
        offsets.y = offsetRect.y + offsetParent.clientTop;
      }
    }
    const htmlOffset = documentElement && !isOffsetParentAnElement && !isFixed ? getHTMLOffset(documentElement, scroll) : createCoords(0);
    return {
      width: rect.width * scale.x,
      height: rect.height * scale.y,
      x: rect.x * scale.x - scroll.scrollLeft * scale.x + offsets.x + htmlOffset.x,
      y: rect.y * scale.y - scroll.scrollTop * scale.y + offsets.y + htmlOffset.y
    };
  }

  function getClientRects(element) {
    return Array.from(element.getClientRects());
  }

  // Gets the entire size of the scrollable document area, even extending outside
  // of the `<html>` and `<body>` rect bounds if horizontally scrollable.
  function getDocumentRect(element) {
    const html = getDocumentElement(element);
    const scroll = getNodeScroll(element);
    const body = element.ownerDocument.body;
    const width = max(html.scrollWidth, html.clientWidth, body.scrollWidth, body.clientWidth);
    const height = max(html.scrollHeight, html.clientHeight, body.scrollHeight, body.clientHeight);
    let x = -scroll.scrollLeft + getWindowScrollBarX(element);
    const y = -scroll.scrollTop;
    if (getComputedStyle$1(body).direction === 'rtl') {
      x += max(html.clientWidth, body.clientWidth) - width;
    }
    return {
      width,
      height,
      x,
      y
    };
  }

  // Safety check: ensure the scrollbar space is reasonable in case this
  // calculation is affected by unusual styles.
  // Most scrollbars leave 15-18px of space.
  const SCROLLBAR_MAX = 25;
  function getViewportRect(element, strategy) {
    const win = getWindow(element);
    const html = getDocumentElement(element);
    const visualViewport = win.visualViewport;
    let width = html.clientWidth;
    let height = html.clientHeight;
    let x = 0;
    let y = 0;
    if (visualViewport) {
      width = visualViewport.width;
      height = visualViewport.height;
      const visualViewportBased = isWebKit();
      if (!visualViewportBased || visualViewportBased && strategy === 'fixed') {
        x = visualViewport.offsetLeft;
        y = visualViewport.offsetTop;
      }
    }
    const windowScrollbarX = getWindowScrollBarX(html);
    // <html> `overflow: hidden` + `scrollbar-gutter: stable` reduces the
    // visual width of the <html> but this is not considered in the size
    // of `html.clientWidth`.
    if (windowScrollbarX <= 0) {
      const doc = html.ownerDocument;
      const body = doc.body;
      const bodyStyles = getComputedStyle(body);
      const bodyMarginInline = doc.compatMode === 'CSS1Compat' ? parseFloat(bodyStyles.marginLeft) + parseFloat(bodyStyles.marginRight) || 0 : 0;
      const clippingStableScrollbarWidth = Math.abs(html.clientWidth - body.clientWidth - bodyMarginInline);
      if (clippingStableScrollbarWidth <= SCROLLBAR_MAX) {
        width -= clippingStableScrollbarWidth;
      }
    } else if (windowScrollbarX <= SCROLLBAR_MAX) {
      // If the <body> scrollbar is on the left, the width needs to be extended
      // by the scrollbar amount so there isn't extra space on the right.
      width += windowScrollbarX;
    }
    return {
      width,
      height,
      x,
      y
    };
  }

  // Returns the inner client rect, subtracting scrollbars if present.
  function getInnerBoundingClientRect(element, strategy) {
    const clientRect = getBoundingClientRect(element, true, strategy === 'fixed');
    const top = clientRect.top + element.clientTop;
    const left = clientRect.left + element.clientLeft;
    const scale = isHTMLElement(element) ? getScale(element) : createCoords(1);
    const width = element.clientWidth * scale.x;
    const height = element.clientHeight * scale.y;
    const x = left * scale.x;
    const y = top * scale.y;
    return {
      width,
      height,
      x,
      y
    };
  }
  function getClientRectFromClippingAncestor(element, clippingAncestor, strategy) {
    let rect;
    if (clippingAncestor === 'viewport') {
      rect = getViewportRect(element, strategy);
    } else if (clippingAncestor === 'document') {
      rect = getDocumentRect(getDocumentElement(element));
    } else if (isElement(clippingAncestor)) {
      rect = getInnerBoundingClientRect(clippingAncestor, strategy);
    } else {
      const visualOffsets = getVisualOffsets(element);
      rect = {
        x: clippingAncestor.x - visualOffsets.x,
        y: clippingAncestor.y - visualOffsets.y,
        width: clippingAncestor.width,
        height: clippingAncestor.height
      };
    }
    return core.rectToClientRect(rect);
  }
  function hasFixedPositionAncestor(element, stopNode) {
    const parentNode = getParentNode(element);
    if (parentNode === stopNode || !isElement(parentNode) || isLastTraversableNode(parentNode)) {
      return false;
    }
    return getComputedStyle$1(parentNode).position === 'fixed' || hasFixedPositionAncestor(parentNode, stopNode);
  }

  // A "clipping ancestor" is an `overflow` element with the characteristic of
  // clipping (or hiding) child elements. This returns all clipping ancestors
  // of the given element up the tree.
  function getClippingElementAncestors(element, cache) {
    const cachedResult = cache.get(element);
    if (cachedResult) {
      return cachedResult;
    }
    let result = getOverflowAncestors(element, [], false).filter(el => isElement(el) && getNodeName(el) !== 'body');
    let currentContainingBlockComputedStyle = null;
    const elementIsFixed = getComputedStyle$1(element).position === 'fixed';
    let currentNode = elementIsFixed ? getParentNode(element) : element;

    // https://developer.mozilla.org/en-US/docs/Web/CSS/Containing_block#identifying_the_containing_block
    while (isElement(currentNode) && !isLastTraversableNode(currentNode)) {
      const computedStyle = getComputedStyle$1(currentNode);
      const currentNodeIsContaining = isContainingBlock(currentNode);
      if (!currentNodeIsContaining && computedStyle.position === 'fixed') {
        currentContainingBlockComputedStyle = null;
      }
      const shouldDropCurrentNode = elementIsFixed ? !currentNodeIsContaining && !currentContainingBlockComputedStyle : !currentNodeIsContaining && computedStyle.position === 'static' && !!currentContainingBlockComputedStyle && (currentContainingBlockComputedStyle.position === 'absolute' || currentContainingBlockComputedStyle.position === 'fixed') || isOverflowElement(currentNode) && !currentNodeIsContaining && hasFixedPositionAncestor(element, currentNode);
      if (shouldDropCurrentNode) {
        // Drop non-containing blocks.
        result = result.filter(ancestor => ancestor !== currentNode);
      } else {
        // Record last containing block for next iteration.
        currentContainingBlockComputedStyle = computedStyle;
      }
      currentNode = getParentNode(currentNode);
    }
    cache.set(element, result);
    return result;
  }

  // Gets the maximum area that the element is visible in due to any number of
  // clipping ancestors.
  function getClippingRect(_ref) {
    let {
      element,
      boundary,
      rootBoundary,
      strategy
    } = _ref;
    const elementClippingAncestors = boundary === 'clippingAncestors' ? isTopLayer(element) ? [] : getClippingElementAncestors(element, this._c) : [].concat(boundary);
    const clippingAncestors = [...elementClippingAncestors, rootBoundary];
    const firstRect = getClientRectFromClippingAncestor(element, clippingAncestors[0], strategy);
    let top = firstRect.top;
    let right = firstRect.right;
    let bottom = firstRect.bottom;
    let left = firstRect.left;
    for (let i = 1; i < clippingAncestors.length; i++) {
      const rect = getClientRectFromClippingAncestor(element, clippingAncestors[i], strategy);
      top = max(rect.top, top);
      right = min(rect.right, right);
      bottom = min(rect.bottom, bottom);
      left = max(rect.left, left);
    }
    return {
      width: right - left,
      height: bottom - top,
      x: left,
      y: top
    };
  }

  function getDimensions(element) {
    const {
      width,
      height
    } = getCssDimensions(element);
    return {
      width,
      height
    };
  }

  function getRectRelativeToOffsetParent(element, offsetParent, strategy) {
    const isOffsetParentAnElement = isHTMLElement(offsetParent);
    const documentElement = getDocumentElement(offsetParent);
    const isFixed = strategy === 'fixed';
    const rect = getBoundingClientRect(element, true, isFixed, offsetParent);
    let scroll = {
      scrollLeft: 0,
      scrollTop: 0
    };
    const offsets = createCoords(0);

    // If the <body> scrollbar appears on the left (e.g. RTL systems). Use
    // Firefox with layout.scrollbar.side = 3 in about:config to test this.
    function setLeftRTLScrollbarOffset() {
      offsets.x = getWindowScrollBarX(documentElement);
    }
    if (isOffsetParentAnElement || !isOffsetParentAnElement && !isFixed) {
      if (getNodeName(offsetParent) !== 'body' || isOverflowElement(documentElement)) {
        scroll = getNodeScroll(offsetParent);
      }
      if (isOffsetParentAnElement) {
        const offsetRect = getBoundingClientRect(offsetParent, true, isFixed, offsetParent);
        offsets.x = offsetRect.x + offsetParent.clientLeft;
        offsets.y = offsetRect.y + offsetParent.clientTop;
      } else if (documentElement) {
        setLeftRTLScrollbarOffset();
      }
    }
    if (isFixed && !isOffsetParentAnElement && documentElement) {
      setLeftRTLScrollbarOffset();
    }
    const htmlOffset = documentElement && !isOffsetParentAnElement && !isFixed ? getHTMLOffset(documentElement, scroll) : createCoords(0);
    const x = rect.left + scroll.scrollLeft - offsets.x - htmlOffset.x;
    const y = rect.top + scroll.scrollTop - offsets.y - htmlOffset.y;
    return {
      x,
      y,
      width: rect.width,
      height: rect.height
    };
  }

  function isStaticPositioned(element) {
    return getComputedStyle$1(element).position === 'static';
  }

  function getTrueOffsetParent(element, polyfill) {
    if (!isHTMLElement(element) || getComputedStyle$1(element).position === 'fixed') {
      return null;
    }
    if (polyfill) {
      return polyfill(element);
    }
    let rawOffsetParent = element.offsetParent;

    // Firefox returns the <html> element as the offsetParent if it's non-static,
    // while Chrome and Safari return the <body> element. The <body> element must
    // be used to perform the correct calculations even if the <html> element is
    // non-static.
    if (getDocumentElement(element) === rawOffsetParent) {
      rawOffsetParent = rawOffsetParent.ownerDocument.body;
    }
    return rawOffsetParent;
  }

  // Gets the closest ancestor positioned element. Handles some edge cases,
  // such as table ancestors and cross browser bugs.
  function getOffsetParent(element, polyfill) {
    const win = getWindow(element);
    if (isTopLayer(element)) {
      return win;
    }
    if (!isHTMLElement(element)) {
      let svgOffsetParent = getParentNode(element);
      while (svgOffsetParent && !isLastTraversableNode(svgOffsetParent)) {
        if (isElement(svgOffsetParent) && !isStaticPositioned(svgOffsetParent)) {
          return svgOffsetParent;
        }
        svgOffsetParent = getParentNode(svgOffsetParent);
      }
      return win;
    }
    let offsetParent = getTrueOffsetParent(element, polyfill);
    while (offsetParent && isTableElement(offsetParent) && isStaticPositioned(offsetParent)) {
      offsetParent = getTrueOffsetParent(offsetParent, polyfill);
    }
    if (offsetParent && isLastTraversableNode(offsetParent) && isStaticPositioned(offsetParent) && !isContainingBlock(offsetParent)) {
      return win;
    }
    return offsetParent || getContainingBlock(element) || win;
  }

  const getElementRects = async function (data) {
    const getOffsetParentFn = this.getOffsetParent || getOffsetParent;
    const getDimensionsFn = this.getDimensions;
    const floatingDimensions = await getDimensionsFn(data.floating);
    return {
      reference: getRectRelativeToOffsetParent(data.reference, await getOffsetParentFn(data.floating), data.strategy),
      floating: {
        x: 0,
        y: 0,
        width: floatingDimensions.width,
        height: floatingDimensions.height
      }
    };
  };

  function isRTL(element) {
    return getComputedStyle$1(element).direction === 'rtl';
  }

  const platform = {
    convertOffsetParentRelativeRectToViewportRelativeRect,
    getDocumentElement,
    getClippingRect,
    getOffsetParent,
    getElementRects,
    getClientRects,
    getDimensions,
    getScale,
    isElement,
    isRTL
  };

  function rectsAreEqual(a, b) {
    return a.x === b.x && a.y === b.y && a.width === b.width && a.height === b.height;
  }

  // https://samthor.au/2021/observing-dom/
  function observeMove(element, onMove) {
    let io = null;
    let timeoutId;
    const root = getDocumentElement(element);
    function cleanup() {
      var _io;
      clearTimeout(timeoutId);
      (_io = io) == null || _io.disconnect();
      io = null;
    }
    function refresh(skip, threshold) {
      if (skip === void 0) {
        skip = false;
      }
      if (threshold === void 0) {
        threshold = 1;
      }
      cleanup();
      const elementRectForRootMargin = element.getBoundingClientRect();
      const {
        left,
        top,
        width,
        height
      } = elementRectForRootMargin;
      if (!skip) {
        onMove();
      }
      if (!width || !height) {
        return;
      }
      const insetTop = floor(top);
      const insetRight = floor(root.clientWidth - (left + width));
      const insetBottom = floor(root.clientHeight - (top + height));
      const insetLeft = floor(left);
      const rootMargin = -insetTop + "px " + -insetRight + "px " + -insetBottom + "px " + -insetLeft + "px";
      const options = {
        rootMargin,
        threshold: max(0, min(1, threshold)) || 1
      };
      let isFirstUpdate = true;
      function handleObserve(entries) {
        const ratio = entries[0].intersectionRatio;
        if (ratio !== threshold) {
          if (!isFirstUpdate) {
            return refresh();
          }
          if (!ratio) {
            // If the reference is clipped, the ratio is 0. Throttle the refresh
            // to prevent an infinite loop of updates.
            timeoutId = setTimeout(() => {
              refresh(false, 1e-7);
            }, 1000);
          } else {
            refresh(false, ratio);
          }
        }
        if (ratio === 1 && !rectsAreEqual(elementRectForRootMargin, element.getBoundingClientRect())) {
          // It's possible that even though the ratio is reported as 1, the
          // element is not actually fully within the IntersectionObserver's root
          // area anymore. This can happen under performance constraints. This may
          // be a bug in the browser's IntersectionObserver implementation. To
          // work around this, we compare the element's bounding rect now with
          // what it was at the time we created the IntersectionObserver. If they
          // are not equal then the element moved, so we refresh.
          refresh();
        }
        isFirstUpdate = false;
      }

      // Older browsers don't support a `document` as the root and will throw an
      // error.
      try {
        io = new IntersectionObserver(handleObserve, {
          ...options,
          // Handle <iframe>s
          root: root.ownerDocument
        });
      } catch (_e) {
        io = new IntersectionObserver(handleObserve, options);
      }
      io.observe(element);
    }
    refresh(true);
    return cleanup;
  }

  /**
   * Automatically updates the position of the floating element when necessary.
   * Should only be called when the floating element is mounted on the DOM or
   * visible on the screen.
   * @returns cleanup function that should be invoked when the floating element is
   * removed from the DOM or hidden from the screen.
   * @see https://floating-ui.com/docs/autoUpdate
   */
  function autoUpdate(reference, floating, update, options) {
    if (options === void 0) {
      options = {};
    }
    const {
      ancestorScroll = true,
      ancestorResize = true,
      elementResize = typeof ResizeObserver === 'function',
      layoutShift = typeof IntersectionObserver === 'function',
      animationFrame = false
    } = options;
    const referenceEl = unwrapElement(reference);
    const ancestors = ancestorScroll || ancestorResize ? [...(referenceEl ? getOverflowAncestors(referenceEl) : []), ...(floating ? getOverflowAncestors(floating) : [])] : [];
    ancestors.forEach(ancestor => {
      ancestorScroll && ancestor.addEventListener('scroll', update, {
        passive: true
      });
      ancestorResize && ancestor.addEventListener('resize', update);
    });
    const cleanupIo = referenceEl && layoutShift ? observeMove(referenceEl, update) : null;
    let reobserveFrame = -1;
    let resizeObserver = null;
    if (elementResize) {
      resizeObserver = new ResizeObserver(_ref => {
        let [firstEntry] = _ref;
        if (firstEntry && firstEntry.target === referenceEl && resizeObserver && floating) {
          // Prevent update loops when using the `size` middleware.
          // https://github.com/floating-ui/floating-ui/issues/1740
          resizeObserver.unobserve(floating);
          cancelAnimationFrame(reobserveFrame);
          reobserveFrame = requestAnimationFrame(() => {
            var _resizeObserver;
            (_resizeObserver = resizeObserver) == null || _resizeObserver.observe(floating);
          });
        }
        update();
      });
      if (referenceEl && !animationFrame) {
        resizeObserver.observe(referenceEl);
      }
      if (floating) {
        resizeObserver.observe(floating);
      }
    }
    let frameId;
    let prevRefRect = animationFrame ? getBoundingClientRect(reference) : null;
    if (animationFrame) {
      frameLoop();
    }
    function frameLoop() {
      const nextRefRect = getBoundingClientRect(reference);
      if (prevRefRect && !rectsAreEqual(prevRefRect, nextRefRect)) {
        update();
      }
      prevRefRect = nextRefRect;
      frameId = requestAnimationFrame(frameLoop);
    }
    update();
    return () => {
      var _resizeObserver2;
      ancestors.forEach(ancestor => {
        ancestorScroll && ancestor.removeEventListener('scroll', update);
        ancestorResize && ancestor.removeEventListener('resize', update);
      });
      cleanupIo == null || cleanupIo();
      (_resizeObserver2 = resizeObserver) == null || _resizeObserver2.disconnect();
      resizeObserver = null;
      if (animationFrame) {
        cancelAnimationFrame(frameId);
      }
    };
  }

  /**
   * Resolves with an object of overflow side offsets that determine how much the
   * element is overflowing a given clipping boundary on each side.
   * - positive = overflowing the boundary by that number of pixels
   * - negative = how many pixels left before it will overflow
   * - 0 = lies flush with the boundary
   * @see https://floating-ui.com/docs/detectOverflow
   */
  const detectOverflow = core.detectOverflow;

  /**
   * Modifies the placement by translating the floating element along the
   * specified axes.
   * A number (shorthand for `mainAxis` or distance), or an axes configuration
   * object may be passed.
   * @see https://floating-ui.com/docs/offset
   */
  const offset = core.offset;

  /**
   * Optimizes the visibility of the floating element by choosing the placement
   * that has the most space available automatically, without needing to specify a
   * preferred placement. Alternative to `flip`.
   * @see https://floating-ui.com/docs/autoPlacement
   */
  const autoPlacement = core.autoPlacement;

  /**
   * Optimizes the visibility of the floating element by shifting it in order to
   * keep it in view when it will overflow the clipping boundary.
   * @see https://floating-ui.com/docs/shift
   */
  const shift = core.shift;

  /**
   * Optimizes the visibility of the floating element by flipping the `placement`
   * in order to keep it in view when the preferred placement(s) will overflow the
   * clipping boundary. Alternative to `autoPlacement`.
   * @see https://floating-ui.com/docs/flip
   */
  const flip = core.flip;

  /**
   * Provides data that allows you to change the size of the floating element —
   * for instance, prevent it from overflowing the clipping boundary or match the
   * width of the reference element.
   * @see https://floating-ui.com/docs/size
   */
  const size = core.size;

  /**
   * Provides data to hide the floating element in applicable situations, such as
   * when it is not in the same clipping context as the reference element.
   * @see https://floating-ui.com/docs/hide
   */
  const hide = core.hide;

  /**
   * Provides data to position an inner element of the floating element so that it
   * appears centered to the reference element.
   * @see https://floating-ui.com/docs/arrow
   */
  const arrow = core.arrow;

  /**
   * Provides improved positioning for inline reference elements that can span
   * over multiple lines, such as hyperlinks or range selections.
   * @see https://floating-ui.com/docs/inline
   */
  const inline = core.inline;

  /**
   * Built-in `limiter` that will stop `shift()` at a certain point.
   */
  const limitShift = core.limitShift;

  /**
   * Computes the `x` and `y` coordinates that will place the floating element
   * next to a given reference element.
   */
  const computePosition = (reference, floating, options) => {
    // This caches the expensive `getClippingElementAncestors` function so that
    // multiple lifecycle resets re-use the same result. It only lives for a
    // single call. If other functions become expensive, we can add them as well.
    const cache = new Map();
    const mergedOptions = {
      platform,
      ...options
    };
    const platformWithCache = {
      ...mergedOptions.platform,
      _c: cache
    };
    return core.computePosition(reference, floating, {
      ...mergedOptions,
      platform: platformWithCache
    });
  };

  exports.arrow = arrow;
  exports.autoPlacement = autoPlacement;
  exports.autoUpdate = autoUpdate;
  exports.computePosition = computePosition;
  exports.detectOverflow = detectOverflow;
  exports.flip = flip;
  exports.getOverflowAncestors = getOverflowAncestors;
  exports.hide = hide;
  exports.inline = inline;
  exports.limitShift = limitShift;
  exports.offset = offset;
  exports.platform = platform;
  exports.shift = shift;
  exports.size = size;

  // Set here as well, so the component scripts find it on pages with an AMD
  // loader.
  window.FloatingUIDOM = exports;

}));

// components/select/select.js
(function () {
  // Constants from Base UI's select, shadcn's reference implementation.
  const SIDE_OFFSET = 4;
  const COLLISION_PADDING = 5;
  const MARGIN = 10; // aligned mode: minimum distance to the viewport edges
  const MIN_HEIGHT = 100; // less room than this -> fall back to popper
  const TRIGGER_COLLISION = 20; // trigger this close to an edge -> popper
  const TOL = 1; // scroll edge tolerance
  const ARROW_TICK_MS = 40; // hovering a scroll arrow scrolls one item per tick
  const SELECTED_DELAY = 400; // mouseup selection stays disabled this long after open

  // The select's element is the positioner (no slot upstream) around the
  // [data-slot=select-content] popup.
  const POPUP = '[data-slot="select-content"]';
  const TRIGGER = '[data-slot="select-trigger"]';
  const ITEM = '[data-slot="select-item"]';
  const ARROWS = '[data-slot="select-scroll-up-button"], [data-slot="select-scroll-down-button"]';

  function isPositioner(el) {
    // The popup is the positioner's slotted child, next to the focus guards.
    return !!el?.querySelector?.(":scope > " + POPUP);
  }

  function allContents() {
    return [...document.querySelectorAll(POPUP)].map((p) => p.parentElement).filter(isPositioner);
  }

  function positionerOf(target) {
    const popup = target && target.closest && target.closest(POPUP);
    return popup && isPositioner(popup.parentElement) ? popup.parentElement : null;
  }

  function triggerFor(content) {
    return document.querySelector(TRIGGER + '[aria-controls="' + content.id + '"]');
  }

  function contentFor(trigger) {
    return document.getElementById(trigger.getAttribute("aria-controls"));
  }

  // The hidden form input sits right before the trigger button.
  function inputFor(trigger) {
    const prev = trigger.previousElementSibling;
    return prev && prev.matches('input[type="hidden"]') ? prev : null;
  }

  // Base UI's Select.ItemText has no slot; it is the item's first child.
  function itemTextOf(item) {
    return item.firstElementChild || item;
  }

  function itemTextOrNull(item) {
    return item ? itemTextOf(item) : null;
  }

  function labelOf(item) {
    return item.getAttribute("data-templ-label") || itemTextOf(item).textContent.trim();
  }

  function valueSpanFor(trigger) {
    return trigger.querySelector('[data-slot="select-value"]');
  }

  function popupFor(content) {
    return content.querySelector(":scope > " + POPUP);
  }

  // Base UI's Select.List has no slot; it is the popup child between the
  // scroll arrows.
  function viewportFor(content) {
    return popupFor(content).querySelector(":scope > :not([data-slot])");
  }

  function clamp(value, min, max) {
    return Math.min(Math.max(value, min), max);
  }

  function maxScrollTop(el) {
    return Math.max(0, el.scrollHeight - el.clientHeight);
  }

  function isAlignMode(content) {
    return content.getAttribute("data-templ-align-item-with-trigger") !== "false";
  }

  // The popup renders the transition status, its positioner the open state.
  function partsOf(content) {
    return { positioner: content, parts: [popupFor(content)] };
  }

  function isOpen(content) {
    return !!content && content.hasAttribute("data-open");
  }



  // The positioner's parent is the portal node, which moves to <body>
  // (shadcn portals it the same way).
  function portalNodeOf(content) {
    return content.parentElement;
  }

  function portal(content) {
    window.templ.portal.render(portalNodeOf(content));
  }

  // SelectPopup's FloatingFocusManager: non modal, focus returns to the
  // trigger on unmount.
  function startFocusManager(content, trigger) {
    if (content._templFocus) {
      content._templFocus.open();
      return;
    }
    content._templFocus = window.templ.focusManager.useFloatingFocusManager({
      floating: content,
      reference: trigger,
      modal: false,
      openInteractionType: content._templOpenMethod === "programmatic" ? null : content._templOpenMethod,
      restoreFocus: true,
      onOpenChange: (open) => requestOpenChange(content, open),
    });
  }

  function stopFocusManager(content) {
    content._templFocus?.unmount();
    content._templFocus = null;
  }

  // ----- list navigation and typeahead ---------------------------------------

  function itemsIn(content) {
    return [...content.querySelectorAll(ITEM)];
  }

  function selectedIndexOf(content) {
    const index = itemsIn(content).findIndex((item) => item.hasAttribute("data-selected"));
    return index === -1 ? null : index;
  }

  // The item's highlight, with SelectItem's roving tabindex.
  function highlight(content, index) {
    content._templActiveIndex = index;
    itemsIn(content).forEach((item, i) => {
      item.toggleAttribute("data-highlighted", i === index);
      item.tabIndex = i === index ? 0 : -1;
    });
  }

  // SelectRoot's useListNavigation and useTypeahead. Disabled items are
  // highlighted (an empty disabledIndices), typeahead skips them, and typing
  // on the closed trigger selects the match.
  function startListNavigation(content, trigger) {
    const popup = popupFor(content);
    const items = () => itemsIn(content);
    const activeIndex = () => content._templActiveIndex ?? null;
    const selectedIndex = () => selectedIndexOf(content);
    const enabled = () => !trigger.disabled && trigger.getAttribute("aria-readonly") !== "true";
    content._templNav = window.templ.listNavigation.useListNavigation({
      floating: popup,
      reference: trigger,
      items,
      activeIndex,
      selectedIndex,
      disabledIndices: [],
      isOpen: () => isOpen(content),
      onNavigate(index) {
        // Retain the highlight while transitioning out.
        if (index === null && !isOpen(content)) return;
        highlight(content, index);
      },
      onOpenChange(open) {
        if (enabled()) requestOpenChange(content, open, "keyboard");
      },
    });
    content._templTypeahead = window.templ.typeahead.useTypeahead({
      elements: [trigger, popup],
      labels: () => items().map(labelOf),
      activeIndex,
      selectedIndex,
      isOpen: () => isOpen(content),
      disabledIndices: (index) => {
        const item = items()[index];
        return !item || item.hasAttribute("disabled") || item.getAttribute("aria-disabled") === "true";
      },
      onMatch(index) {
        if (!enabled()) return;
        if (isOpen(content)) {
          highlight(content, index);
          content._templNav.sync();
        } else {
          selectItem(content, items()[index]);
        }
      },
    });
  }

  function stopListNavigation(content) {
    content._templNav?.cleanup();
    content._templTypeahead?.cleanup();
    content._templNav = null;
    content._templTypeahead = null;
  }

  // Clears everything a previous open left behind on the positioner and popup.
  function resetInlineStyles(content) {
    ["left", "right", "top", "bottom", "height", "maxHeight", "marginTop", "marginBottom"].forEach(
      (prop) => (content.style[prop] = ""),
    );
    const popup = popupFor(content);
    if (popup) popup.style.height = "";
  }


  // Overlays the menu so the selected item sits on the trigger with its text
  // aligned to the trigger text. Port of Base UI's SelectPopup align logic.
  // Runs after the first positioning pass, which sets the CSS variables;
  // returns false when Base UI falls back to popper positioning.
  function positionAligned(content, trigger) {
    const popup = popupFor(content);
    const viewport = viewportFor(content);
    const valueEl = valueSpanFor(trigger);
    const textEl =
      itemTextOrNull(content.querySelector(ITEM + "[data-selected]")) ||
      itemTextOrNull(content.querySelector(ITEM));

    const docEl = document.documentElement;
    const triggerRect = trigger.getBoundingClientRect();
    const positionerRect = content.getBoundingClientRect(); // natural size from the popper pass
    // The list's natural height, measured before the aligned styles stretch
    // the popup (scrollHeight can never report less than the client height).
    const naturalScrollHeight = viewport.scrollHeight;
    const popupStyles = window.getComputedStyle(popup);
    const borderBottom = parseFloat(popupStyles.borderBottomWidth) || 0;
    const maxPopupHeight = parseFloat(popupStyles.maxHeight) || Infinity;
    const viewportHeight = docEl.clientHeight - MARGIN * 2;
    const viewportWidth = docEl.clientWidth;
    const availableSpaceBeneathTrigger = viewportHeight - triggerRect.bottom + triggerRect.height;

    let alignedLeft = triggerRect.left;
    let offsetY = 0;
    let textRect = null;
    if (textEl && valueEl) {
      const valueRect = valueEl.getBoundingClientRect();
      textRect = textEl.getBoundingClientRect();
      alignedLeft = positionerRect.left + (valueRect.left - textRect.left);
      offsetY =
        textRect.top - positionerRect.top + textRect.height / 2 -
        (valueRect.top - triggerRect.top + valueRect.height / 2);
    }

    const idealHeight = availableSpaceBeneathTrigger + offsetY + MARGIN + borderBottom;
    let height = Math.min(viewportHeight, idealHeight);
    const maxHeight = viewportHeight - MARGIN * 2;
    const scrollTop = idealHeight - height;

    content.style.left =
      clamp(
        alignedLeft,
        COLLISION_PADDING,
        Math.max(COLLISION_PADDING, viewportWidth - COLLISION_PADDING - positionerRect.width),
      ) + "px";
    content.style.height = height + "px";
    content.style.maxHeight = "none";
    content.style.marginTop = MARGIN + "px";
    content.style.marginBottom = MARGIN + "px";
    popup.style.height = "100%";

    const max = maxScrollTop(viewport);
    const isTopPositioned = scrollTop >= max - TOL;

    if (isTopPositioned) {
      height = Math.min(viewportHeight, positionerRect.height) - (scrollTop - max);
    }

    if (
      triggerRect.top < TRIGGER_COLLISION ||
      triggerRect.bottom > viewportHeight - TRIGGER_COLLISION ||
      Math.ceil(height) + TOL < Math.min(naturalScrollHeight, MIN_HEIGHT)
    ) {
      return false;
    }

    content._templReachedMax = false;

    if (isTopPositioned) {
      const topOffset = Math.max(0, viewportHeight - idealHeight);
      content.style.top = (positionerRect.height >= maxHeight ? 0 : topOffset) + "px";
      content.style.height = height + "px";
      viewport.scrollTop = maxScrollTop(viewport);
    } else {
      content.style.top = "auto";
      content.style.bottom = "0px";
      viewport.scrollTop = scrollTop;
    }

    if (textRect) {
      const clampedY = clamp(
        positionerRect.height > 0
          ? ((textRect.top + textRect.height / 2 - positionerRect.top) / positionerRect.height) * 100
          : 50,
        0,
        100,
      );
      popup.style.setProperty("--transform-origin", "50% " + clampedY + "%");
    }

    if (height >= viewportHeight || height >= maxPopupHeight) {
      content._templReachedMax = true;
    }
    return true;
  }

  // SelectPositioner: useAnchorPositioning with the dropdown collision
  // avoidance for the popper mode. While the popup is aligned with the trigger
  // (alignItemWithTrigger, not for touch opens) the positioner is fixed, its
  // side is "none", anchor tracking is off and positionAligned places it,
  // once per open. When that does not fit, the select falls back to the
  // popper mode until it unmounts.
  function startAutoPositioning(content, trigger) {
    stopAutoPositioning(content);
    const popup = popupFor(content);
    if (!popup || !viewportFor(content)) return Promise.resolve();
    const alignActive = isAlignMode(content) && content._templOpenMethod !== "touch" && !content._templAlignFallback;
    content._templAligned = false;
    resetInlineStyles(content);
    let placed = false;
    const positioning = window.templ.anchorPositioning.useAnchorPositioning({
      anchor: trigger,
      positioner: content,
      parts: [content, popup],
      side: content.getAttribute("data-templ-side") || "bottom",
      align: content.getAttribute("data-templ-align") || "center",
      sideOffset: parseFloat(content.getAttribute("data-templ-side-offset")) || SIDE_OFFSET,
      alignOffset: parseFloat(content.getAttribute("data-templ-align-offset")) || 0,
      collisionAvoidance: { fallbackAxisSide: "none" },
      disableAnchorTracking: alignActive,
      applyPosition: () => !alignActive,
      onPosition(result, side) {
        trigger.setAttribute("data-popup-side", side);
        if (!alignActive) {
          updateScrollArrows(content);
          return;
        }
        if (placed) return;
        placed = true;
        [content, popup].forEach((part) => part.setAttribute("data-side", "none"));
        if (positionAligned(content, trigger)) {
          content._templAligned = true;
          updateScrollArrows(content);
          return;
        }
        content._templAlignFallback = true;
        startAutoPositioning(content, trigger);
      },
    });
    content._templPositionCleanup = positioning.cleanup;
    return positioning.positioned;
  }

  function stopAutoPositioning(content) {
    if (!content._templPositionCleanup) return;
    content._templPositionCleanup();
    content._templPositionCleanup = null;
  }

  // ----- scroll arrows + capped grow-on-scroll (Base UI behavior) -----------

  function updateScrollArrows(content) {
    const viewport = viewportFor(content);
    const up = content.querySelector('[data-slot="select-scroll-up-button"]');
    const down = content.querySelector('[data-slot="select-scroll-down-button"]');
    if (!viewport || !up || !down) return;
    const max = maxScrollTop(viewport);
    up.classList.toggle("hidden", max <= 0 || viewport.scrollTop <= TOL);
    down.classList.toggle("hidden", max <= 0 || viewport.scrollTop >= max - TOL);
  }

  // In aligned mode scrolling first consumes the remaining space toward the
  // viewport edge (capped by the popup's max-height), then scrolls the list.
  function handleAlignedScroll(content) {
    const viewport = viewportFor(content);
    const popup = popupFor(content);
    if (!viewport || !popup) return;

    const isTopPositioned = content.style.top === "0px";
    const isBottomPositioned = content.style.bottom === "0px";

    if (content._templReachedMax || !content._templAligned || (!isTopPositioned && !isBottomPositioned)) {
      updateScrollArrows(content);
      return;
    }

    const currentHeight = content.getBoundingClientRect().height;
    const maxPopupHeight = parseFloat(window.getComputedStyle(popup).maxHeight) || Infinity;
    const maxAvailableHeight = Math.min(
      document.documentElement.clientHeight - MARGIN * 2,
      maxPopupHeight,
    );

    const scrollTop = viewport.scrollTop;
    const max = maxScrollTop(viewport);

    let nextScrollTop = null;
    const diff = isTopPositioned ? max - scrollTop : scrollTop;
    const nextHeight = Math.min(currentHeight + diff, maxAvailableHeight);

    if (diff <= TOL) {
      const heightDelta = clamp(diff, 0, maxAvailableHeight - currentHeight);
      if (heightDelta > 0) {
        content.style.height = currentHeight + heightDelta + "px";
      }
      viewport.scrollTop = isTopPositioned ? maxScrollTop(viewport) : 0;
      if (maxAvailableHeight - (currentHeight + heightDelta) <= TOL) {
        content._templReachedMax = true;
      }
      updateScrollArrows(content);
      return;
    }

    if (maxAvailableHeight - nextHeight > TOL) {
      nextScrollTop = isTopPositioned ? Infinity : 0;
    } else if (isBottomPositioned && scrollTop < max) {
      const overshoot = currentHeight + diff - maxAvailableHeight;
      nextScrollTop = scrollTop - (diff - overshoot);
    }

    const nextPositionerHeight = Math.ceil(nextHeight);
    if (nextPositionerHeight !== 0) {
      content.style.height = nextPositionerHeight + "px";
    }

    if (nextScrollTop != null) {
      const target = clamp(nextScrollTop, 0, maxScrollTop(viewport));
      if (Math.abs(viewport.scrollTop - target) > TOL) {
        viewport.scrollTop = target;
      }
    }

    if (nextPositionerHeight >= maxAvailableHeight - TOL) {
      content._templReachedMax = true;
    }
    updateScrollArrows(content);
  }

  // Hovering a scroll arrow scrolls one item per tick, keeping the next item
  // clear of the arrow overlay (Base UI's getTargetScrollTop).
  function targetScrollTop(items, isUp, scrollTop, clientHeight, arrowHeight, max) {
    if (isUp) {
      let firstVisibleIndex = 0;
      const visibleTop = scrollTop + arrowHeight - TOL;
      for (let i = 0; i < items.length; i += 1) {
        if (items[i].offsetTop >= visibleTop) {
          firstVisibleIndex = i;
          break;
        }
      }
      const targetIndex = Math.max(0, firstVisibleIndex - 1);
      const target = items[targetIndex];
      return targetIndex < firstVisibleIndex && target
        ? clamp(target.offsetTop - arrowHeight, 0, max)
        : 0;
    }

    let lastVisibleIndex = items.length - 1;
    const visibleBottom = scrollTop + clientHeight - arrowHeight + TOL;
    for (let i = 0; i < items.length; i += 1) {
      if (items[i].offsetTop + items[i].offsetHeight > visibleBottom) {
        lastVisibleIndex = Math.max(0, i - 1);
        break;
      }
    }
    const targetIndex = Math.min(items.length - 1, lastVisibleIndex + 1);
    const target = items[targetIndex];
    return targetIndex > lastVisibleIndex && target
      ? clamp(target.offsetTop + target.offsetHeight - clientHeight + arrowHeight, 0, max)
      : max;
  }

  let arrowTimer = null;

  function stopArrowScroll() {
    clearTimeout(arrowTimer);
    arrowTimer = null;
  }

  function arrowScrollStep(content, isUp, arrow) {
    const viewport = viewportFor(content);
    if (!viewport) return;
    updateScrollArrows(content);
    const max = maxScrollTop(viewport);
    const scrollTop = clamp(viewport.scrollTop, 0, max);
    if (scrollTop === (isUp ? 0 : max)) {
      stopArrowScroll();
      return;
    }
    const items = [...content.querySelectorAll(ITEM)];
    viewport.scrollTop = targetScrollTop(
      items,
      isUp,
      scrollTop,
      viewport.clientHeight,
      arrow.offsetHeight || 0,
      max,
    );
    arrowTimer = setTimeout(() => arrowScrollStep(content, isUp, arrow), ARROW_TICK_MS);
  }

  document.addEventListener("mouseover", (e) => {
    if (!(e.target instanceof Element)) return;
    const arrow = e.target.closest(ARROWS);
    if (!arrow || arrowTimer) return;
    const content = positionerOf(arrow);
    if (content) arrowScrollStep(content, arrow.matches('[data-slot="select-scroll-up-button"]'), arrow);
  });

  document.addEventListener("mouseout", (e) => {
    if (!(e.target instanceof Element)) return;
    if (e.target.closest(ARROWS)) {
      stopArrowScroll();
    }
  });

  // ----- open / close -------------------------------------------------------

  function open(content, trigger, openMethod) {
    allContents().forEach((c) => {
      if (c !== content) close(c);
    });
    content._templOpenMethod = openMethod || "programmatic";
    // A press on the trigger can open the popup under the pointer (aligned
    // mode). Mouseup selection stays disabled briefly so releasing over the
    // selected item or a neighboring item doesn't commit an accidental
    // selection (Base UI's selectionRef + SELECTED_DELAY). Dragging can
    // re-arm unselected mouseup sooner, see the pointermove handler.
    content._templSelection = {
      allowSelectedMouseUp: false,
      allowUnselectedMouseUp: false,
      dragY: 0,
    };
    clearTimeout(content._templSelectedDelay);
    content._templSelectedDelay = setTimeout(() => {
      content._templSelection.allowSelectedMouseUp = true;
      content._templSelection.allowUnselectedMouseUp = true;
    }, SELECTED_DELAY);
    portal(content);
    content._templDismiss ??= window.templ.dismiss.useDismiss({
      floating: content,
      reference: trigger,
      onOpenChange: (open) => requestOpenChange(content, open),
    });
    startFocusManager(content, trigger);
    content.hidden = false;

    // Positioned first, then the enter animation plays in place.
    const finish = () => {
      const popup = popupFor(content);
      if (content.hidden || !content.isConnected) return;
      // useAnchoredPopupScrollLock measures the positioned popup for touch opens.
      content._templReleaseScroll?.();
      content._templReleaseScroll = window.templ.scrollLock.anchoredPopup(
        true, content._templOpenMethod === "touch", content, trigger,
      );
      window.templ.transition.open(partsOf(content));
      trigger.setAttribute("aria-expanded", "true");
      trigger.setAttribute("data-popup-open", "");
      trigger.setAttribute("data-pressed", "");
      content._templNav?.open();
      content._templTypeahead?.reset();
    };
    startAutoPositioning(content, trigger).then(finish, finish);
  }

  function close(content) {
    if (content.hidden) return;
    content._templDismiss?.();
    content._templDismiss = null;
    stopArrowScroll();
    clearTimeout(content._templSelectedDelay);
    content._templSelection = {
      allowSelectedMouseUp: false,
      allowUnselectedMouseUp: false,
      dragY: 0,
    };
    content._templFocus?.close();
    content._templNav?.close();
    content._templTypeahead?.reset();
    // Aligned mode has no exit animation (animate-none, like shadcn), so
    // the close completes on the next frame. Positioned until it unmounts,
    // and the alignment fallback holds until then too. Unmounting the focus
    // manager returns focus.
    window.templ.transition.close(partsOf(content), popupFor(content), () => {
      stopAutoPositioning(content);
      stopFocusManager(content);
      highlight(content, null);
      content._templAlignFallback = false;
      content.hidden = true;
    });
    content._templReleaseScroll?.();
    content._templReleaseScroll = null;
    const trigger = triggerFor(content);
    if (trigger) {
      trigger.setAttribute("aria-expanded", "false");
      trigger.removeAttribute("data-popup-open");
      trigger.removeAttribute("data-pressed");
    }
  }

  function closeAll() {
    allContents().forEach(close);
  }

  function requestOpenChange(content, nextOpen, openMethod) {
    if (!content || isOpen(content) === nextOpen) return false;
    const accepted = content.dispatchEvent(
      new CustomEvent("select-open-change", {
        bubbles: true,
        cancelable: true,
        detail: {
          open: nextOpen,
          openMethod: nextOpen ? openMethod || "programmatic" : null,
        },
      }),
    );
    if (!accepted || content.hasAttribute("data-templ-open")) return false;
    const trigger = triggerFor(content);
    if (nextOpen && trigger) open(content, trigger, openMethod);
    else if (!nextOpen) close(content);
    return true;
  }

  function selectItem(content, item) {
    const trigger = triggerFor(content);
    if (!trigger) return;
  if (trigger.getAttribute("aria-readonly") === "true") return;
    const value = item.getAttribute("data-templ-value") || "";
    const label = labelOf(item);

    const accepted = trigger.dispatchEvent(
      new CustomEvent("select-change", {
        bubbles: true,
        cancelable: true,
        detail: { value: value, label: label },
      }),
    );
    if (!accepted) return;

    // Controlled: the Base UI value prop, the owner commits.
    if (!trigger.hasAttribute("data-templ-value")) {
      content.querySelectorAll(ITEM).forEach((i) => {
      i.removeAttribute("data-selected");
      i.setAttribute("aria-selected", "false");
      });
      item.setAttribute("data-selected", "");
      item.setAttribute("aria-selected", "true");

      const span = valueSpanFor(trigger);
      if (span) span.textContent = label;
      trigger.removeAttribute("data-placeholder");

      const input = inputFor(trigger);
      if (input && input.value !== value) {
        input.value = value;
        input.dispatchEvent(new Event("change", { bubbles: true }));
      }
    }
    requestOpenChange(content, false);
  }

  // Shows the selected item's label in the trigger (server only knows the
  // value, the label lives in the item).
  window.templ.lifecycle.register(TRIGGER, {
    init(trigger) {
      const content = contentFor(trigger);
      if (!isPositioner(content)) return;
      startListNavigation(content, trigger);
      const checked = content.querySelector(ITEM + "[data-selected]");
      if (checked) {
        const label = labelOf(checked);
        const span = valueSpanFor(trigger);
        if (span && span.textContent.trim() !== label) span.textContent = label;
        if (trigger.hasAttribute("data-placeholder")) trigger.removeAttribute("data-placeholder");
      }
      // Server-side open state (Base UI open or defaultOpen). A server open
      // has no pointer, so it is programmatic.
      if (content.getAttribute("data-templ-open") === "true" || content.hasAttribute("data-templ-default-open")) {
        open(content, trigger, "programmatic");
      }
    },
  });
  // A content unmounts with its portal owner: a portaled one is removed from
  // <body> then.
  window.templ.lifecycle.register(POPUP, {
    destroy(popup) {
      const content = popup.parentElement;
      if (!isPositioner(content)) return;
      stopListNavigation(content);
      stopAutoPositioning(content);
      content._templReleaseScroll?.();
      content._templReleaseScroll = null;
      content._templDismiss?.();
      stopFocusManager(content);
      window.templ.portal.remove(portalNodeOf(content));
    },
  });

  // ----- events -------------------------------------------------------------

  // Pointer interactions toggle and dismiss on PRESS, exactly like Base UI.
  // Click is never used for open/close, so the stray click the browser fires
  // on <body> after the menu opened over the trigger is naturally harmless.
  function toggle(trigger, openMethod) {
    const content = contentFor(trigger);
    if (!content) return;
    requestOpenChange(content, !isOpen(content), openMethod);
  }

  // Pendant of floating-ui useClick's pointerTypeRef: pointerdown marks the
  // trigger, the click that follows the same press is skipped. A click
  // without the mark (a <label for> forward, keyboard activation, or a
  // programmatic .click()) toggles instead.
  const pressedTriggers = new WeakSet();

  function isMouseWithinBounds(e, el) {
    const rect = el.getBoundingClientRect();
    return (
      e.clientX >= rect.left &&
      e.clientX <= rect.right &&
      e.clientY >= rect.top &&
      e.clientY <= rect.bottom
    );
  }

  // Pendant of SelectTrigger's mousedown handler: the press that opened the
  // popup cancels the open again when released outside the trigger and the
  // popup positioner.
  function armCancelOpen(trigger, content) {
    // Firefox can fire the mouseup upon mousedown, hence the deferred attach.
    setTimeout(() => {
      document.addEventListener(
        "mouseup",
        (e) => {
          const target = e.target instanceof Element ? e.target : null;
          // Don't treat the release as an outside press when it lands on the
          // trigger or inside the popup (or their children).
          if (target && (trigger.contains(target) || content.contains(target))) return;
          if (isMouseWithinBounds(e, trigger)) return;
          close(content);
        },
        { once: true },
      );
    }, 0);
  }

  document.addEventListener("pointerdown", (e) => {
    if (e.button !== 0 || !(e.target instanceof Element)) return;
    const trigger = e.target.closest(TRIGGER);
    if (trigger) {
      // Touch opens on the click that fires at release (Base UI opens on
      // the compat mousedown, which for touch also fires post-touchend).
      // Opening at press would put the aligned popup under the still-down
      // finger, and the tap's click, hit-tested at the release point,
      // would land on the item above the trigger and instantly commit it.
      trigger._templOpenMethod = e.pointerType;
      if (e.pointerType === "touch") return;
      pressedTriggers.add(trigger);
      // Keep the browser from focusing the trigger button, focus lives on
      // the selected item while the listbox is open (Base UI focus scope).
      e.preventDefault();
      if (!trigger.disabled) {
        const content = contentFor(trigger);
        if (content) {
          if (isOpen(content)) {
            requestOpenChange(content, false);
          } else {
            requestOpenChange(content, true, e.pointerType);
            armCancelOpen(trigger, content);
          }
        }
      }
      return;
    }
    // Pendant of SelectItem's allowMouseSelectionRef: a real pointer click
    // only commits when its press started on the item. The stray click the
    // browser hit-tests onto the popup that just opened over the trigger
    // (touch fires its compatibility click at the tap position) never did.
    const item = e.target.closest(ITEM);
    if (item) {
      item._templPointerType = e.pointerType;
      item._templAllowMouseSelection = true;
      const content = positionerOf(item);
      if (content && content._templSelection) content._templSelection.dragY = 0;
    }
  });

  document.addEventListener("pointerover", (e) => {
    if (!(e.target instanceof Element)) return;
    const item = e.target.closest(ITEM);
    if (item) item._templPointerType = e.pointerType;
  });

  document.addEventListener("pointercancel", (e) => {
    if (!(e.target instanceof Element)) return;
    const trigger = e.target.closest(TRIGGER);
    if (!trigger) return;
    trigger._templOpenMethod = null;
    pressedTriggers.delete(trigger);
  });

  document.addEventListener("click", (e) => {
    if (!(e.target instanceof Element)) return;
    const trigger = e.target.closest(TRIGGER);
    if (trigger) {
      if (pressedTriggers.has(trigger)) {
        pressedTriggers.delete(trigger);
        return;
      }
      const openMethod = trigger._templOpenMethod || (e.detail === 0 ? "keyboard" : "mouse");
      trigger._templOpenMethod = null;
      if (!trigger.disabled) {
        toggle(trigger, openMethod);
      }
      return;
    }

    const item = e.target.closest(ITEM);
    if (item) {
      const content = positionerOf(item);
      if (!content) return;
      // Virtual clicks (detail 0: keyboard, assistive technology, .click())
      // represent explicit activation and always commit; so do touch clicks,
      // whose press necessarily started on the item.
      const isMouseClick = (item._templPointerType || "mouse") !== "touch";
      const isVirtualClick = e.detail === 0;
      const isInvalidMouseClick =
        isMouseClick && !isVirtualClick && !item._templAllowMouseSelection;
      item._templAllowMouseSelection = false;
      if (item.hasAttribute("data-disabled") || isInvalidMouseClick) return;
      selectItem(content, item);
    }
  });

  // Pendant of SelectItem's mouseup: releasing a press that started on the
  // trigger commits the item under the pointer (press trigger, drag, release
  // to select), once the SELECTED_DELAY / drag guards allow it. Touch never
  // selects on mouseup, only on click.
  document.addEventListener("mouseup", (e) => {
    if (!(e.target instanceof Element)) return;
    const item = e.target.closest(ITEM);
    if (!item) return;
    const content = positionerOf(item);
    const selection = content && content._templSelection;
    if (!selection) return;
    selection.dragY = 0;
    if (item.hasAttribute("data-disabled") || item._templPointerType === "touch") return;
    // Regular clicks are committed by the click event.
    if (item._templAllowMouseSelection) return;
    const selected = item.hasAttribute("data-selected");
    if (
      (!selection.allowSelectedMouseUp && selected) ||
      (!selection.allowUnselectedMouseUp && !selected)
    ) {
      return;
    }
    item._templAllowMouseSelection = true;
    item.click();
    item._templAllowMouseSelection = false;
  });

  document.addEventListener("keydown", (e) => {
    if (!(e.target instanceof Element)) return;
    const trigger = e.target.closest(TRIGGER);
    if (trigger) {
      pressedTriggers.delete(trigger); // like useClick's onKeyDown reset
      trigger._templOpenMethod = null;
      return;
    }
    // SelectItem is a button: Enter and Space commit the item. A Space that
    // continues a typeahead never gets here, useTypeahead stops it.
    const item = e.target.closest(ITEM);
    if (!item || (e.key !== "Enter" && e.key !== " ")) return;
    const content = positionerOf(item);
    if (!content) return;
    e.preventDefault();
    if (!item.hasAttribute("data-disabled")) selectItem(content, item);
  });

  // SelectItem's onPointerMove, the highlight itself follows the pointer
  // through the list navigation.
  document.addEventListener("pointermove", (e) => {
    if (!(e.target instanceof Element)) return;
    const item = e.target.closest(ITEM);
    if (!item) return;
    // Dragging with the button held re-arms unselected mouseup selection
    // before SELECTED_DELAY has elapsed, once the drag covers >= 8px.
    if (e.pointerType === "mouse" && e.buttons === 1) {
      const content = positionerOf(item);
      if (content && content._templSelection) {
        content._templSelection.dragY += e.movementY;
        if (content._templSelection.dragY ** 2 >= 64) {
          content._templSelection.allowUnselectedMouseUp = true;
        }
      }
    }
  });

  window.addEventListener(
    "scroll",
    (e) => {
      const inMenu = e.target instanceof Element && positionerOf(e.target);
      if (inMenu) {
        if (inMenu._templAligned) {
          handleAlignedScroll(inMenu);
        } else {
          updateScrollArrows(inMenu);
        }
        return;
      }
    },
    true,
  );

})();

