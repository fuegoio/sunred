import type { ReactNode } from "react";
import {
  type LucideIcon,
  AppWindow,
  AtSign,
  Container,
  House,
  KeyRound,
  Layers,
  Network,
  Newspaper,
  Rocket,
  Rss,
  Server,
  SlidersHorizontal,
  Terminal,
  Users,
} from "lucide-react";

// Icon names usable in MDX frontmatter (`icon: house`) and in meta.json
// (`---[icon]Guides---`). Resolved for the sidebar page tree in lib/source.ts.
const icons: Record<string, LucideIcon> = {
  house: House,
  rocket: Rocket,
  "at-sign": AtSign,
  "app-window": AppWindow,
  rss: Rss,
  newspaper: Newspaper,
  users: Users,
  terminal: Terminal,
  "key-round": KeyRound,
  layers: Layers,
  container: Container,
  "sliders-horizontal": SlidersHorizontal,
  network: Network,
  server: Server,
};

export function resolveIcon(name: string | undefined): ReactNode {
  const Icon = name ? icons[name] : undefined;
  return Icon ? <Icon /> : undefined;
}
