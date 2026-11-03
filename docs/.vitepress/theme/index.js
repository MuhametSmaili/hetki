import { h } from "vue";
import DefaultTheme from "vitepress/theme-without-fonts";
import HomeHero from "./HomeHero.vue";
import HomeSections from "./HomeSections.vue";
import "./icons.css";
import "./style.css";

export default {
  extends: DefaultTheme,
  Layout: () =>
    h(DefaultTheme.Layout, null, {
      "home-hero-before": () => h(HomeHero),
      "home-features-after": () => h(HomeSections),
    }),
};
