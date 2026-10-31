import { Select, ListBox, Label, Switch, Accordion } from "@heroui/react";
import { useId, useState, type ReactNode } from "react";
export function Picker({
  label,
  value,
  values,
  onChange,
  icon,
  disabled = false,
  description,
}: {
  label: string;
  value: string;
  values: [string, string][];
  onChange: (v: string) => void;
  icon?: ReactNode;
  disabled?: boolean;
  description?: string;
}) {
  return (
    <Select
      aria-label={label}
      aria-description={description}
      isDisabled={disabled}
      value={value}
      onChange={(v) => onChange(String(v))}
    >
      <Select.Trigger>
        {icon && (
          <span className="picker-icon" aria-hidden="true">
            {icon}
          </span>
        )}
        <Select.Value />
        <Select.Indicator />
      </Select.Trigger>
      <Select.Popover>
        <ListBox>
          {values.map(([id, text]) => (
            <ListBox.Item key={id} id={id} textValue={text}>
              {text}
              <ListBox.ItemIndicator />
            </ListBox.Item>
          ))}
        </ListBox>
      </Select.Popover>
    </Select>
  );
}
export function Toggle({
  label,
  value,
  onChange,
}: {
  label: string;
  value: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <Switch
      className="inline-toggle"
      isSelected={value}
      onChange={onChange}
      size="sm"
    >
      <Switch.Content>
        <Switch.Control>
          <Switch.Thumb />
        </Switch.Control>
        <Label>{label}</Label>
      </Switch.Content>
    </Switch>
  );
}
export function Fold({
  title,
  children,
  className,
  open,
  onOpenChange,
  label,
}: {
  title: ReactNode;
  children: ReactNode | (() => ReactNode);
  className?: string;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  label?: string;
}) {
  const id = useId();
  const [localOpen, setLocalOpen] = useState(false);
  const expanded = open ?? localOpen;
  return (
    <Accordion
      className={className}
      hideSeparator
      expandedKeys={expanded ? [id] : []}
      onExpandedChange={(keys) => {
        const next = keys.has(id);
        setLocalOpen(next);
        onOpenChange?.(next);
      }}
    >
      <Accordion.Item id={id}>
        <Accordion.Heading>
          <Accordion.Trigger aria-label={label}>
            {title}
            <Accordion.Indicator />
          </Accordion.Trigger>
        </Accordion.Heading>
        <Accordion.Panel>
          {expanded && (
            <Accordion.Body>
              {typeof children === "function" ? children() : children}
            </Accordion.Body>
          )}
        </Accordion.Panel>
      </Accordion.Item>
    </Accordion>
  );
}
