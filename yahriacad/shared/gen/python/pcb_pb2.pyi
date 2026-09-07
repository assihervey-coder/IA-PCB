from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Point(_message.Message):
    __slots__ = ("x", "y")
    X_FIELD_NUMBER: _ClassVar[int]
    Y_FIELD_NUMBER: _ClassVar[int]
    x: float
    y: float
    def __init__(self, x: _Optional[float] = ..., y: _Optional[float] = ...) -> None: ...

class BBox(_message.Message):
    __slots__ = ("min_x", "min_y", "max_x", "max_y")
    MIN_X_FIELD_NUMBER: _ClassVar[int]
    MIN_Y_FIELD_NUMBER: _ClassVar[int]
    MAX_X_FIELD_NUMBER: _ClassVar[int]
    MAX_Y_FIELD_NUMBER: _ClassVar[int]
    min_x: float
    min_y: float
    max_x: float
    max_y: float
    def __init__(self, min_x: _Optional[float] = ..., min_y: _Optional[float] = ..., max_x: _Optional[float] = ..., max_y: _Optional[float] = ...) -> None: ...

class PadRef(_message.Message):
    __slots__ = ("component_ref", "pad_name", "position", "layer", "width_mm", "height_mm", "rotation_deg")
    COMPONENT_REF_FIELD_NUMBER: _ClassVar[int]
    PAD_NAME_FIELD_NUMBER: _ClassVar[int]
    POSITION_FIELD_NUMBER: _ClassVar[int]
    LAYER_FIELD_NUMBER: _ClassVar[int]
    WIDTH_MM_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_MM_FIELD_NUMBER: _ClassVar[int]
    ROTATION_DEG_FIELD_NUMBER: _ClassVar[int]
    component_ref: str
    pad_name: str
    position: Point
    layer: int
    width_mm: float
    height_mm: float
    rotation_deg: float
    def __init__(self, component_ref: _Optional[str] = ..., pad_name: _Optional[str] = ..., position: _Optional[_Union[Point, _Mapping]] = ..., layer: _Optional[int] = ..., width_mm: _Optional[float] = ..., height_mm: _Optional[float] = ..., rotation_deg: _Optional[float] = ...) -> None: ...

class TrackPoint(_message.Message):
    __slots__ = ("position", "layer")
    POSITION_FIELD_NUMBER: _ClassVar[int]
    LAYER_FIELD_NUMBER: _ClassVar[int]
    position: Point
    layer: int
    def __init__(self, position: _Optional[_Union[Point, _Mapping]] = ..., layer: _Optional[int] = ...) -> None: ...

class TrackSegment(_message.Message):
    __slots__ = ("a", "b", "width_mm")
    A_FIELD_NUMBER: _ClassVar[int]
    B_FIELD_NUMBER: _ClassVar[int]
    WIDTH_MM_FIELD_NUMBER: _ClassVar[int]
    a: TrackPoint
    b: TrackPoint
    width_mm: float
    def __init__(self, a: _Optional[_Union[TrackPoint, _Mapping]] = ..., b: _Optional[_Union[TrackPoint, _Mapping]] = ..., width_mm: _Optional[float] = ...) -> None: ...

class Via(_message.Message):
    __slots__ = ("position", "from_layer", "to_layer", "diameter_mm", "drill_mm")
    POSITION_FIELD_NUMBER: _ClassVar[int]
    FROM_LAYER_FIELD_NUMBER: _ClassVar[int]
    TO_LAYER_FIELD_NUMBER: _ClassVar[int]
    DIAMETER_MM_FIELD_NUMBER: _ClassVar[int]
    DRILL_MM_FIELD_NUMBER: _ClassVar[int]
    position: Point
    from_layer: int
    to_layer: int
    diameter_mm: float
    drill_mm: float
    def __init__(self, position: _Optional[_Union[Point, _Mapping]] = ..., from_layer: _Optional[int] = ..., to_layer: _Optional[int] = ..., diameter_mm: _Optional[float] = ..., drill_mm: _Optional[float] = ...) -> None: ...

class BoardSpec(_message.Message):
    __slots__ = ("width_mm", "height_mm", "layer_count", "grid_resolution_mm", "layer_names")
    WIDTH_MM_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_MM_FIELD_NUMBER: _ClassVar[int]
    LAYER_COUNT_FIELD_NUMBER: _ClassVar[int]
    GRID_RESOLUTION_MM_FIELD_NUMBER: _ClassVar[int]
    LAYER_NAMES_FIELD_NUMBER: _ClassVar[int]
    width_mm: float
    height_mm: float
    layer_count: int
    grid_resolution_mm: float
    layer_names: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, width_mm: _Optional[float] = ..., height_mm: _Optional[float] = ..., layer_count: _Optional[int] = ..., grid_resolution_mm: _Optional[float] = ..., layer_names: _Optional[_Iterable[str]] = ...) -> None: ...

class ComponentSpec(_message.Message):
    __slots__ = ("ref", "footprint", "position", "rotation_deg", "fixed", "bbox_mm", "height_mm")
    REF_FIELD_NUMBER: _ClassVar[int]
    FOOTPRINT_FIELD_NUMBER: _ClassVar[int]
    POSITION_FIELD_NUMBER: _ClassVar[int]
    ROTATION_DEG_FIELD_NUMBER: _ClassVar[int]
    FIXED_FIELD_NUMBER: _ClassVar[int]
    BBOX_MM_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_MM_FIELD_NUMBER: _ClassVar[int]
    ref: str
    footprint: str
    position: Point
    rotation_deg: float
    fixed: bool
    bbox_mm: BBox
    height_mm: float
    def __init__(self, ref: _Optional[str] = ..., footprint: _Optional[str] = ..., position: _Optional[_Union[Point, _Mapping]] = ..., rotation_deg: _Optional[float] = ..., fixed: _Optional[bool] = ..., bbox_mm: _Optional[_Union[BBox, _Mapping]] = ..., height_mm: _Optional[float] = ...) -> None: ...

class NetSpec(_message.Message):
    __slots__ = ("name", "net_class", "pads", "min_track_width_mm", "clearance_mm")
    NAME_FIELD_NUMBER: _ClassVar[int]
    NET_CLASS_FIELD_NUMBER: _ClassVar[int]
    PADS_FIELD_NUMBER: _ClassVar[int]
    MIN_TRACK_WIDTH_MM_FIELD_NUMBER: _ClassVar[int]
    CLEARANCE_MM_FIELD_NUMBER: _ClassVar[int]
    name: str
    net_class: str
    pads: _containers.RepeatedCompositeFieldContainer[PadRef]
    min_track_width_mm: float
    clearance_mm: float
    def __init__(self, name: _Optional[str] = ..., net_class: _Optional[str] = ..., pads: _Optional[_Iterable[_Union[PadRef, _Mapping]]] = ..., min_track_width_mm: _Optional[float] = ..., clearance_mm: _Optional[float] = ...) -> None: ...

class PlacementRequest(_message.Message):
    __slots__ = ("board", "components", "strategy")
    BOARD_FIELD_NUMBER: _ClassVar[int]
    COMPONENTS_FIELD_NUMBER: _ClassVar[int]
    STRATEGY_FIELD_NUMBER: _ClassVar[int]
    board: BoardSpec
    components: _containers.RepeatedCompositeFieldContainer[ComponentSpec]
    strategy: str
    def __init__(self, board: _Optional[_Union[BoardSpec, _Mapping]] = ..., components: _Optional[_Iterable[_Union[ComponentSpec, _Mapping]]] = ..., strategy: _Optional[str] = ...) -> None: ...

class PlacementResult(_message.Message):
    __slots__ = ("placed", "total_wirelength_mm", "score", "strategy")
    PLACED_FIELD_NUMBER: _ClassVar[int]
    TOTAL_WIRELENGTH_MM_FIELD_NUMBER: _ClassVar[int]
    SCORE_FIELD_NUMBER: _ClassVar[int]
    STRATEGY_FIELD_NUMBER: _ClassVar[int]
    placed: _containers.RepeatedCompositeFieldContainer[ComponentSpec]
    total_wirelength_mm: float
    score: float
    strategy: str
    def __init__(self, placed: _Optional[_Iterable[_Union[ComponentSpec, _Mapping]]] = ..., total_wirelength_mm: _Optional[float] = ..., score: _Optional[float] = ..., strategy: _Optional[str] = ...) -> None: ...

class RouteRequest(_message.Message):
    __slots__ = ("board", "nets", "placed", "strategy", "net_filter")
    BOARD_FIELD_NUMBER: _ClassVar[int]
    NETS_FIELD_NUMBER: _ClassVar[int]
    PLACED_FIELD_NUMBER: _ClassVar[int]
    STRATEGY_FIELD_NUMBER: _ClassVar[int]
    NET_FILTER_FIELD_NUMBER: _ClassVar[int]
    board: BoardSpec
    nets: _containers.RepeatedCompositeFieldContainer[NetSpec]
    placed: _containers.RepeatedCompositeFieldContainer[ComponentSpec]
    strategy: str
    net_filter: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, board: _Optional[_Union[BoardSpec, _Mapping]] = ..., nets: _Optional[_Iterable[_Union[NetSpec, _Mapping]]] = ..., placed: _Optional[_Iterable[_Union[ComponentSpec, _Mapping]]] = ..., strategy: _Optional[str] = ..., net_filter: _Optional[_Iterable[str]] = ...) -> None: ...

class RouteNetResult(_message.Message):
    __slots__ = ("net", "segments", "vias", "length_mm", "completed", "drc_violations")
    NET_FIELD_NUMBER: _ClassVar[int]
    SEGMENTS_FIELD_NUMBER: _ClassVar[int]
    VIAS_FIELD_NUMBER: _ClassVar[int]
    LENGTH_MM_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_FIELD_NUMBER: _ClassVar[int]
    DRC_VIOLATIONS_FIELD_NUMBER: _ClassVar[int]
    net: str
    segments: _containers.RepeatedCompositeFieldContainer[TrackSegment]
    vias: _containers.RepeatedCompositeFieldContainer[Via]
    length_mm: float
    completed: bool
    drc_violations: int
    def __init__(self, net: _Optional[str] = ..., segments: _Optional[_Iterable[_Union[TrackSegment, _Mapping]]] = ..., vias: _Optional[_Iterable[_Union[Via, _Mapping]]] = ..., length_mm: _Optional[float] = ..., completed: _Optional[bool] = ..., drc_violations: _Optional[int] = ...) -> None: ...

class OptimizeRequest(_message.Message):
    __slots__ = ("board", "nets", "routes", "objectives")
    BOARD_FIELD_NUMBER: _ClassVar[int]
    NETS_FIELD_NUMBER: _ClassVar[int]
    ROUTES_FIELD_NUMBER: _ClassVar[int]
    OBJECTIVES_FIELD_NUMBER: _ClassVar[int]
    board: BoardSpec
    nets: _containers.RepeatedCompositeFieldContainer[NetSpec]
    routes: _containers.RepeatedCompositeFieldContainer[RouteNetResult]
    objectives: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, board: _Optional[_Union[BoardSpec, _Mapping]] = ..., nets: _Optional[_Iterable[_Union[NetSpec, _Mapping]]] = ..., routes: _Optional[_Iterable[_Union[RouteNetResult, _Mapping]]] = ..., objectives: _Optional[_Iterable[str]] = ...) -> None: ...

class ProgressEvent(_message.Message):
    __slots__ = ("job_id", "stage", "current_net", "percent", "message", "partial", "done", "error")
    JOB_ID_FIELD_NUMBER: _ClassVar[int]
    STAGE_FIELD_NUMBER: _ClassVar[int]
    CURRENT_NET_FIELD_NUMBER: _ClassVar[int]
    PERCENT_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    PARTIAL_FIELD_NUMBER: _ClassVar[int]
    DONE_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    job_id: str
    stage: str
    current_net: str
    percent: float
    message: str
    partial: RouteNetResult
    done: bool
    error: str
    def __init__(self, job_id: _Optional[str] = ..., stage: _Optional[str] = ..., current_net: _Optional[str] = ..., percent: _Optional[float] = ..., message: _Optional[str] = ..., partial: _Optional[_Union[RouteNetResult, _Mapping]] = ..., done: _Optional[bool] = ..., error: _Optional[str] = ...) -> None: ...

class HealthRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class HealthResponse(_message.Message):
    __slots__ = ("status", "version", "device", "model_loaded")
    STATUS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    DEVICE_FIELD_NUMBER: _ClassVar[int]
    MODEL_LOADED_FIELD_NUMBER: _ClassVar[int]
    status: str
    version: str
    device: str
    model_loaded: bool
    def __init__(self, status: _Optional[str] = ..., version: _Optional[str] = ..., device: _Optional[str] = ..., model_loaded: _Optional[bool] = ...) -> None: ...
