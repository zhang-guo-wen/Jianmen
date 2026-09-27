export const TABLE_COLUMN_WIDTHS = {
  address: 200,
  url: 240,
  status: 88,
  number: 104,
  note: 200,
  time: 176,
  group: 144,
  identifier: 136,
  actionsCompact: 96,
  actions: 160,
  actionsWide: 224,
  actionsExtraWide: 280,
} as const;

interface TableColumnPreset {
  width?: number;
  minWidth?: number;
  align: 'left' | 'right';
  headerAlign: 'left' | 'right';
  fixed?: 'right';
  showOverflowTooltip?: true;
}

const leftAligned = {
  align: 'left',
  headerAlign: 'left',
} as const;

const rightFixed = {
  align: 'right',
  headerAlign: 'right',
  fixed: 'right',
} as const;

export const TABLE_COLUMNS = {
  address: {
    ...leftAligned,
    width: TABLE_COLUMN_WIDTHS.address,
    showOverflowTooltip: true,
  },
  url: {
    ...leftAligned,
    minWidth: TABLE_COLUMN_WIDTHS.url,
    showOverflowTooltip: true,
  },
  status: {
    ...leftAligned,
    width: TABLE_COLUMN_WIDTHS.status,
  },
  number: {
    ...leftAligned,
    width: TABLE_COLUMN_WIDTHS.number,
  },
  note: {
    ...leftAligned,
    minWidth: TABLE_COLUMN_WIDTHS.note,
    showOverflowTooltip: true,
  },
  time: {
    ...leftAligned,
    width: TABLE_COLUMN_WIDTHS.time,
    showOverflowTooltip: true,
  },
  group: {
    ...leftAligned,
    width: TABLE_COLUMN_WIDTHS.group,
    showOverflowTooltip: true,
  },
  identifier: {
    ...leftAligned,
    width: TABLE_COLUMN_WIDTHS.identifier,
    showOverflowTooltip: true,
  },
  actionsCompact: {
    ...rightFixed,
    width: TABLE_COLUMN_WIDTHS.actionsCompact,
  },
  actions: {
    ...rightFixed,
    width: TABLE_COLUMN_WIDTHS.actions,
  },
  actionsWide: {
    ...rightFixed,
    width: TABLE_COLUMN_WIDTHS.actionsWide,
  },
  actionsExtraWide: {
    ...rightFixed,
    width: TABLE_COLUMN_WIDTHS.actionsExtraWide,
  },
} as const satisfies Record<string, TableColumnPreset>;
