// Tile state management types and utilities

/** Tile display states */
export enum TileState {
    /** Normal tile in graph */
    NORMAL = 'normal',
    /** Tile is focused and expanded */
    FOCUSED = 'focused',
    /** Tile is dimmed (another tile is focused) */
    DIMMED = 'dimmed',
    /** Tile is being dragged */
    DRAGGING = 'dragging',
    /** Tile is hovered */
    HOVERED = 'hovered'
}

/** Tile dimensions for different states */
export interface TileDimensions {
    width: number;
    height: number;
    /** Scale factor relative to default size */
    scale?: number;
}

/** Tile decoration visibility */
export interface TileDecorations {
    header: boolean;
    footer: boolean;
}
