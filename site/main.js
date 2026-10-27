(() => {
  const hero = document.querySelector(".folder--hero");
  const tabs = document.querySelectorAll(".hanging-tabs .tab");
  const sections = [...document.querySelectorAll("main section[id]")];

  if (hero) {
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (reduce) {
      hero.classList.add("is-in");
    } else {
      requestAnimationFrame(() => hero.classList.add("is-in"));
    }
  }

  const setCurrent = (id) => {
    tabs.forEach((tab) => {
      const href = tab.getAttribute("href") || "";
      const match = href === `#${id}` || (id === "top" && href === "#top");
      if (match) tab.setAttribute("aria-current", "page");
      else tab.removeAttribute("aria-current");
    });
  };

  if ("IntersectionObserver" in window) {
    const spy = new IntersectionObserver(
      (entries) => {
        const visible = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0];
        if (visible?.target?.id) setCurrent(visible.target.id);
      },
      { rootMargin: "-35% 0px -50% 0px", threshold: [0.2, 0.5, 0.8] }
    );
    sections.forEach((s) => spy.observe(s));
  }

  tabs.forEach((tab) => {
    tab.addEventListener("click", () => {
      const href = tab.getAttribute("href") || "";
      if (!href.startsWith("#")) return;
      const id = href.slice(1);
      if (id) setCurrent(id);
    });
  });
})();
