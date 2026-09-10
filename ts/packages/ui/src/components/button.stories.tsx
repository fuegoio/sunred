import type { Meta, StoryObj } from "@storybook/react";
import { Plus, ChevronRight, LoaderCircle } from "lucide-react";

import { Button, buttonVariants } from "@workspace/ui/components/button";

const meta: Meta<typeof Button> = {
  title: "UI/Button",
  component: Button,
  tags: ["autodocs"],
  args: {
    children: "Subscribe",
  },
  argTypes: {
    variant: {
      control: "select",
      options: ["default", "outline", "secondary", "ghost", "destructive", "link"],
    },
    size: {
      control: "select",
      options: ["default", "xs", "sm", "lg", "icon", "icon-xs", "icon-sm", "icon-lg"],
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { variant: "default", size: "default" },
};

export const PrimaryStates: Story = {
  render: () => (
    <div id="primary-actions" className="flex flex-wrap items-center gap-4">
      <Button>Subscribe</Button>
      <Button disabled>Subscribe</Button>
      <Button disabled aria-busy="true">
        <LoaderCircle className="animate-spin" aria-hidden="true" />
        Subscribing
      </Button>
      <a href="#primary-actions" className={buttonVariants()}>
        Open reader <ChevronRight aria-hidden="true" />
      </a>
    </div>
  ),
};

export const Variants: Story = {
  render: () => (
    <div className="flex flex-wrap items-center gap-4">
      <Button variant="default">Default</Button>
      <Button variant="outline">Outline</Button>
      <Button variant="secondary">Secondary</Button>
      <Button variant="ghost">Ghost</Button>
      <Button variant="destructive">Destructive</Button>
      <Button variant="link">Link</Button>
    </div>
  ),
};

export const Sizes: Story = {
  render: () => (
    <div className="flex flex-wrap items-center gap-4">
      <Button size="xs">Extra small</Button>
      <Button size="sm">Small</Button>
      <Button size="default">Default</Button>
      <Button size="lg">Large</Button>
      <Button size="icon-sm" aria-label="Add">
        <Plus />
      </Button>
      <Button size="icon" aria-label="Add">
        <Plus />
      </Button>
      <Button size="icon-lg" aria-label="Add">
        <Plus />
      </Button>
    </div>
  ),
};

export const WithIcon: Story = {
  render: () => (
    <div className="flex flex-wrap items-center gap-4">
      <Button variant="default">
        <Plus data-icon="inline-start" /> New feed
      </Button>
      <Button variant="default">
        Continue <ChevronRight data-icon="inline-end" />
      </Button>
      <Button variant="outline" size="icon" aria-label="Add">
        <Plus />
      </Button>
    </div>
  ),
};
