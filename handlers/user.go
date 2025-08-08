package handlers

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/yourusername/trough/models"
)

type UserHandler struct {
	userRepo  models.UserRepositoryInterface
	imageRepo models.ImageRepositoryInterface
}

func NewUserHandler(userRepo models.UserRepositoryInterface, imageRepo models.ImageRepositoryInterface) *UserHandler {
	return &UserHandler{
		userRepo:  userRepo,
		imageRepo: imageRepo,
	}
}

func (h *UserHandler) GetProfile(c *fiber.Ctx) error {
	username := c.Params("username")
	if username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Username required",
		})
	}

	user, err := h.userRepo.GetByUsername(username)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	return c.JSON(user.ToResponse())
}

func (h *UserHandler) GetUserImages(c *fiber.Ctx) error {
	username := c.Params("username")
	if username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Username required",
		})
	}

	user, err := h.userRepo.GetByUsername(username)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	
	limit := 20

	images, total, err := h.imageRepo.GetUserImages(user.ID, page, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch user images",
		})
	}

	return c.JSON(models.FeedResponse{
		Images: images,
		Page:   page,
		Total:  total,
	})
}