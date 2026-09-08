"use client";

import { useEffect, useRef, useState } from "react";
import * as THREE from "three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";
import { useProjectStore } from "@/lib/store/project-store";
import { estimateFootprintSize } from "@/lib/utils/geometry-utils";

const BOARD_THICKNESS_MM = 1.6;
const COMPONENT_HEIGHT_MM = 1.6;

interface ViewerApi {
  resetView: () => void;
  setWireframe: (value: boolean) => void;
}

export default function Viewer3D() {
  const mountRef = useRef<HTMLDivElement | null>(null);
  const apiRef = useRef<ViewerApi | null>(null);
  const [wireframe, setWireframe] = useState(false);
  const layout = useProjectStore((s) => s.layout);

  useEffect(() => {
    const mount = mountRef.current;
    if (!mount || !layout) return;

    const W = layout.board.width_mm;
    const H = layout.board.height_mm;

    const scene = new THREE.Scene();
    scene.background = new THREE.Color(0x0b1220);

    const camera = new THREE.PerspectiveCamera(
      50,
      Math.max(mount.clientWidth, 1) / Math.max(mount.clientHeight, 1),
      0.1,
      2000,
    );
    const initialCamPos = new THREE.Vector3(W * 1.5, Math.max(W, H) * 1.15, H * 1.8);
    camera.position.copy(initialCamPos);

    const renderer = new THREE.WebGLRenderer({ antialias: true });
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    renderer.setSize(mount.clientWidth, mount.clientHeight);
    mount.appendChild(renderer.domElement);

    const controls = new OrbitControls(camera, renderer.domElement);
    controls.target.set(W / 2, 0, H / 2);
    controls.enableDamping = true;
    controls.dampingFactor = 0.08;
    controls.update();

    const disposables: Array<{ dispose: () => void }> = [];
    const solidMaterials: THREE.MeshStandardMaterial[] = [];

    // Lumières : ambiance + directionnelle principale + rappel cyan.
    scene.add(new THREE.AmbientLight(0xffffff, 0.55));
    const keyLight = new THREE.DirectionalLight(0xffffff, 1.1);
    keyLight.position.set(W, 90, H * 0.6);
    scene.add(keyLight);
    const fillLight = new THREE.DirectionalLight(0x22d3ee, 0.3);
    fillLight.position.set(-W, 40, -H);
    scene.add(fillLight);

    // Grille de sol.
    const gridHelper = new THREE.GridHelper(Math.max(W, H) * 4, Math.ceil((Math.max(W, H) * 4) / 5), 0x24314f, 0x16203a);
    gridHelper.position.y = -BOARD_THICKNESS_MM - 0.05;
    scene.add(gridHelper);
    disposables.push(gridHelper.geometry);
    const gridMaterials = Array.isArray(gridHelper.material) ? gridHelper.material : [gridHelper.material];
    for (const m of gridMaterials) disposables.push(m);

    // Plaque PCB (vert, épaisseur 1,6 mm, face supérieure à y = 0).
    const boardGeo = new THREE.BoxGeometry(W, BOARD_THICKNESS_MM, H);
    const boardMat = new THREE.MeshStandardMaterial({ color: 0x0f7a3d, roughness: 0.55, metalness: 0.1 });
    const board = new THREE.Mesh(boardGeo, boardMat);
    board.position.set(W / 2, -BOARD_THICKNESS_MM / 2, H / 2);
    scene.add(board);
    disposables.push(boardGeo);
    solidMaterials.push(boardMat);

    const copperMat = new THREE.MeshStandardMaterial({ color: 0xf59e0b, roughness: 0.35, metalness: 0.65 });
    const goldMat = new THREE.MeshStandardMaterial({ color: 0xffd166, roughness: 0.3, metalness: 0.7 });
    const componentMat = new THREE.MeshStandardMaterial({ color: 0x64748b, roughness: 0.6, metalness: 0.2 });
    solidMaterials.push(copperMat, goldMat, componentMat);

    // Pistes : boîtes fines (0,035 mm) le long de chaque segment.
    for (const track of layout.tracks) {
      const mat = track.layer === 0 ? copperMat : goldMat;
      const y = track.layer === 0 ? 0.018 : 0.052;
      for (let i = 0; i + 1 < track.points.length; i++) {
        const a = track.points[i];
        const b = track.points[i + 1];
        const dx = b.x - a.x;
        const dz = b.y - a.y;
        const len = Math.hypot(dx, dz);
        if (len < 1e-4) continue;
        const geo = new THREE.BoxGeometry(track.width, 0.035, len);
        const mesh = new THREE.Mesh(geo, mat);
        mesh.position.set((a.x + b.x) / 2, y, (a.y + b.y) / 2);
        mesh.rotation.y = Math.atan2(dx, dz);
        scene.add(mesh);
        disposables.push(geo);
      }
    }

    // Vias : cylindres or traversants.
    for (const via of layout.vias) {
      const geo = new THREE.CylinderGeometry(via.diameter / 2, via.diameter / 2, BOARD_THICKNESS_MM + 0.12, 20);
      const mesh = new THREE.Mesh(geo, goldMat);
      mesh.position.set(via.x, -BOARD_THICKNESS_MM / 2, via.y);
      scene.add(mesh);
      disposables.push(geo);
    }

    // Composants : boîtes (corps d'empreinte × hauteur 1,6 mm) + arêtes.
    const edgeMat = new THREE.LineBasicMaterial({ color: 0x9db7e8 });
    disposables.push(edgeMat);
    for (const comp of layout.components) {
      const size = estimateFootprintSize(comp.footprint);
      const geo = new THREE.BoxGeometry(size.w, COMPONENT_HEIGHT_MM, size.h);
      const mesh = new THREE.Mesh(geo, componentMat);
      mesh.position.set(comp.x, COMPONENT_HEIGHT_MM / 2, comp.y);
      mesh.rotation.y = -THREE.MathUtils.degToRad(comp.rotation);
      scene.add(mesh);
      disposables.push(geo);

      const edges = new THREE.EdgesGeometry(geo);
      const outline = new THREE.LineSegments(edges, edgeMat);
      outline.position.copy(mesh.position);
      outline.rotation.copy(mesh.rotation);
      scene.add(outline);
      disposables.push(edges);
    }

    renderer.setAnimationLoop(() => {
      controls.update();
      renderer.render(scene, camera);
    });

    const onResize = () => {
      const w = mount.clientWidth;
      const h = mount.clientHeight;
      if (w === 0 || h === 0) return;
      camera.aspect = w / h;
      camera.updateProjectionMatrix();
      renderer.setSize(w, h);
    };
    const observer = new ResizeObserver(onResize);
    observer.observe(mount);

    apiRef.current = {
      resetView: () => {
        camera.position.copy(initialCamPos);
        controls.target.set(W / 2, 0, H / 2);
        controls.update();
      },
      setWireframe: (value: boolean) => {
        for (const material of solidMaterials) material.wireframe = value;
      },
    };

    return () => {
      observer.disconnect();
      renderer.setAnimationLoop(null);
      controls.dispose();
      for (const d of disposables) d.dispose();
      for (const m of solidMaterials) m.dispose();
      renderer.dispose();
      if (renderer.domElement.parentElement === mount) {
        mount.removeChild(renderer.domElement);
      }
      apiRef.current = null;
    };
  }, [layout]);

  const handleReset = () => apiRef.current?.resetView();

  const handleWireframe = () => {
    const next = !wireframe;
    setWireframe(next);
    apiRef.current?.setWireframe(next);
  };

  return (
    <div className="viewer-wrap panel">
      <div className="toolbar viewer-toolbar">
        <button type="button" className="btn btn-secondary btn-sm" onClick={handleReset}>
          Réinitialiser la vue
        </button>
        <button
          type="button"
          className={`btn ${wireframe ? "btn-primary" : "btn-secondary"} btn-sm`}
          onClick={handleWireframe}
        >
          {wireframe ? "Rendu solide" : "Fil de fer"}
        </button>
        <span className="viewer-hint">Glisser : orbite — Molette : zoom — Clic droit : déplacement</span>
      </div>
      <div ref={mountRef} className="viewer-mount" data-testid="viewer-canvas" />
    </div>
  );
}
